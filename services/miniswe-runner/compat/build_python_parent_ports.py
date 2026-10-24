"""Construct explicit pure-Python dependency-contract ports without SDK imports."""
from pathlib import Path, PurePosixPath
import argparse
import base64
import csv
import email
import io
import json
import zipfile
from urllib.request import build_opener

from build_port_wheel import PublicRedirects, public_url, sha256


def build(spec, output, upstream_directory=None):
    if upstream_directory:
        raw=(upstream_directory/spec['upstream_filename']).read_bytes()
    else:
        public_url(spec['upstream_url'])
        with build_opener(PublicRedirects()).open(spec['upstream_url'],timeout=60) as response:
            public_url(response.url)
            raw=response.read(64*1024*1024+1)
    if len(raw)>64*1024*1024 or sha256(raw)!=spec['upstream_wheel_sha256']:
        raise ValueError('Parent wheel identity differs')
    files,modes={},{}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist())>30000 or sum(m.file_size for m in archive.infolist())>256*1024*1024:
            raise ValueError('Unexpected parent wheel bounds')
        for member in archive.infolist():
            path=PurePosixPath(member.filename)
            if path.is_absolute() or '..' in path.parts or '\\' in member.filename:
                raise ValueError('Parent wheel member escapes root')
            if member.is_dir():continue
            if member.filename in files or (member.external_attr>>16)&0o170000==0o120000:
                raise ValueError('Duplicate/linked parent wheel member')
            files[member.filename]=archive.read(member)
            modes[member.filename]=member.external_attr>>16
    old=spec['distribution_directory']+'-'+spec['upstream_version']+'.dist-info'
    new=spec['distribution_directory']+'-'+spec['local_version']+'.dist-info'
    records=list(csv.reader(io.StringIO(files[old+'/RECORD'].decode())))
    if len(records)!=len(files) or {r[0] for r in records}!=set(files):
        raise ValueError('Original parent RECORD membership differs')
    for name,checksum,size in records:
        if name==old+'/RECORD':
            if checksum or size:raise ValueError('Original self RECORD differs')
            continue
        if checksum!='sha256='+base64.urlsafe_b64encode(bytes.fromhex(sha256(files[name]))).rstrip(b'=').decode() or len(files[name])!=int(size):
            raise ValueError('Original parent RECORD content differs')
    sources={name:sha256(value) for name,value in files.items() if name.endswith('.py')}
    manifest=sha256(json.dumps(sources,sort_keys=True,separators=(',',':')).encode())
    if len(sources)!=spec['unchanged_python_file_count'] or manifest!=spec['unchanged_python_manifest_sha256']:
        raise ValueError('Original parent Python source manifest differs')
    original=dict(files)
    metadata=files[old+'/METADATA'].replace(b'\r\n',b'\n')
    parsed=email.message_from_bytes(metadata)
    if parsed['Name'].lower()!=spec['name'] or parsed['Version']!=spec['upstream_version']:
        raise ValueError('Original parent distribution identity differs')
    header,separator,payload=metadata.partition(b'\n\n')
    if not separator:raise ValueError('Unexpected parent metadata envelope')
    edits=[{'old':'Version: '+spec['upstream_version'],'new':'Version: '+spec['local_version']},
           *spec['metadata_edits']]
    lines=header.splitlines()
    for edit in edits:
        before,after=edit['old'].encode(),edit['new'].encode()
        if lines.count(before)!=1:raise ValueError('Parent contract source differs')
        lines=[after if line==before else line for line in lines]
    files[old+'/METADATA']=b'\n'.join(lines)+b'\n\n'+payload+('\n\nHAI explicit compatibility port: '+spec['profile']+'\n').encode()
    wheel_lines=files[old+'/WHEEL'].decode().splitlines()
    if 'Root-Is-Purelib: true' not in wheel_lines or 'Tag: '+spec['wheel_tag'] not in wheel_lines:
        raise ValueError('Unexpected original parent wheel platform')
    files[old+'/WHEEL']=('\n'.join('Generator: HAI source-bound parent compatibility port' if line.startswith('Generator:') else line for line in wheel_lines)+'\n').encode()
    if any(files[name]!=value for name,value in original.items() if name not in {old+'/METADATA',old+'/WHEEL',old+'/RECORD'}):
        raise ValueError('Parent source/resource/license/entrypoint changed')
    files.pop(old+'/RECORD')
    files={name.replace(old+'/',new+'/',1):value for name,value in files.items()}
    modes={name.replace(old+'/',new+'/',1):mode for name,mode in modes.items()}
    receipt={**spec,'vendor_code_executed':False,'Python_sources_modified':0,
             'behavior_or_data_provider_acceptance':False}
    files[new+'/HAI_PARENT_PORT.json']=(json.dumps(receipt,indent=2)+'\n').encode()
    stream=io.StringIO(newline='');writer=csv.writer(stream,lineterminator='\n')
    for name,value in sorted(files.items()):
        digest=base64.urlsafe_b64encode(bytes.fromhex(sha256(value))).rstrip(b'=').decode()
        writer.writerow([name,'sha256='+digest,len(value)])
    writer.writerow([new+'/RECORD','','']);files[new+'/RECORD']=stream.getvalue().encode()
    result=io.BytesIO()
    with zipfile.ZipFile(result,'w',compression=zipfile.ZIP_STORED) as archive:
        for name,value in sorted(files.items()):
            info=zipfile.ZipInfo(name,(1980,1,1,0,0,0));info.create_system=3
            info.external_attr=modes.get(name,0o100644)<<16;archive.writestr(info,value)
    name=spec['distribution_directory']+'-'+spec['local_version']+'-'+spec['wheel_tag']+'.whl'
    output.mkdir(parents=True,exist_ok=True)
    with (output/name).open('xb') as destination:destination.write(result.getvalue())
    return {'name':spec['name'],'filename':name,'sha256':sha256(result.getvalue()),
            'bytes':len(result.getvalue()),'upstream_sha256':spec['upstream_wheel_sha256'],
            'unchanged_python_files':len(sources),'all_source_resources_modes_licenses_entrypoints_preserved':True,
            'vendor_code_executed':False,'runtime_qualified':False}


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-directory',type=Path,required=True)
    parser.add_argument('--upstream-directory',type=Path)
    args=parser.parse_args()
    specs=json.loads(Path(__file__).with_name('python-parent-port-specs.json').read_text(encoding='utf-8'))
    print(json.dumps([build(spec,args.output_directory,args.upstream_directory) for spec in specs]))
