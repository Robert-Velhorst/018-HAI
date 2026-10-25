"""Export only a source- and installed-case-qualified runtime build context."""
from pathlib import Path,PurePosixPath
import hashlib,io,json,os,re,shutil,sys,tarfile

def main():
    if sys.platform!='linux' or os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':raise RuntimeError('Use the owned hosted qualification workflow')
    root=Path(sys.argv[1]).resolve(strict=True);temporary=Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
    if root==temporary or not root.is_relative_to(temporary):raise ValueError('Expected dedicated qualification output')
    sha=lambda value:hashlib.sha256(value).hexdigest()
    build=json.loads((root/'outputs/qualification.json').read_text());cases=json.loads((root/'installed-case-reports/installed-case-qualification.json').read_text());spec=json.loads((root/'inputs/sources.json').read_text())
    if build['build_returncode']!=0 or build['UID']!=10001 or build['GCC']!='16.2.0' or build['Python']!='3.15.1':raise ValueError('Unexpected package build profile')
    if build['original_test_files_changed'] or build['source_changes_outside_reviewed_Debian_patches']:raise ValueError('Original source or cases changed outside reviewed patches')
    if cases['counts']!={'PASS':14,'SKIP':0,'XFAIL':2,'XPASS':0,'FAIL':0,'ERROR':0} or not cases['publisher_expected_outcomes_matched'] or not cases['all_selected_cases_executed'] or cases['original_case_or_helper_files_changed']:raise ValueError('Original installed cases must qualify first')
    if not cases['ABI_symbol_proof']['all_baseline_public_symbol_versions_preserved']:raise ValueError('Baseline ELF exports must qualify')
    for item in spec['inputs']:
        if sha((root/'inputs'/item['name']).read_bytes())!=item['sha256']:raise ValueError('Original source input changed')
    with tarfile.open(root/'inputs/acl_2.4.0.orig.tar.xz',mode='r:xz') as tar:
        tests={m.name.removeprefix('acl-2.4.0/'):sha(tar.extractfile(m).read()) for m in tar.getmembers() if m.isfile() and m.name.startswith('acl-2.4.0/test/')}
        module=tar.extractfile('acl-2.4.0/test/Makemodule.am').read().decode()
    expected=set(re.findall(r'test/[\w./-]+\.run',module.split('XFAIL_TESTS =',1)[1].split('\nTESTS =',1)[0]))
    if expected!={'test/nfs/nfsacl.run','test/nfs/nfs-dir.run'} or tests!=build['original_test_files']:raise ValueError('Original upstream case profile changed')
    if {r['case'] for r in cases['cases']}!={n for n in tests if n.endswith('.run')}:raise ValueError('Original case coverage changed')
    for item in cases['cases']:
        expected_status='XFAIL' if item['case'] in expected and item['returncode'] not in [0,77,99] else 'PASS' if item['returncode']==0 else 'UNEXPECTED'
        log=root/'installed-case-reports'/(item['case'].replace('/','__')+'.log')
        if item['status']!=expected_status or sha(log.read_bytes())!=item['log_sha256']:raise ValueError('Original case result or log changed')
    name='libacl1_2.4.0-1_amd64.deb';package=root/'outputs'/name
    row=next(r for r in build['packages'] if r['name']==name)
    if sha(package.read_bytes())!=row['sha256']:raise ValueError('Qualified package changed')
    raw=package.read_bytes()
    if raw[:8]!=b'!<arch>\n':raise ValueError('Unexpected package container')
    position=8;members={}
    while position<len(raw):
        header=raw[position:position+60]
        if len(header)!=60 or header[58:60]!=b'`\n':raise ValueError('Malformed package header')
        size=int(header[48:58].decode().strip());key=header[:16].decode().strip().removesuffix('/')
        members[key]=raw[position+60:position+60+size];position+=60+size;position+=position%2
    values=[v for k,v in members.items() if k.startswith('data.tar.')]
    if len(values)!=1:raise ValueError('Package payload missing')
    files={}
    with tarfile.open(fileobj=io.BytesIO(values[0]),mode='r:*') as tar:
        for item in tar.getmembers():
            if not item.isfile():continue
            rel=PurePosixPath(item.name.removeprefix('./'))
            if rel.is_absolute() or '..' in rel.parts or rel.parts[0]!='usr':raise ValueError('Unexpected payload path')
            files[rel.as_posix()]=sha(tar.extractfile(item).read())
    if any(cases['installed_files_sha256'].get('/'+n)!=h for n,h in files.items()):raise ValueError('Installed files do not match package')
    output=root/'runtime-candidate';output.mkdir()
    shutil.copyfile(package,output/name)
    verifier=Path(__file__).with_name('verify_runtime_candidate.py');shutil.copyfile(verifier,output/'verify_candidate.py')
    manifest={'schema':1,'package':'libacl1','version':'2.4.0-1','package_sha256':row['sha256'],'verifier_sha256':sha(verifier.read_bytes()),'baseline_package_version':cases['ABI_symbol_proof']['baseline']['package_version'],'all_baseline_public_symbol_versions_preserved':True,'installed_vendor_case_counts':cases['counts'],'payload_files_sha256':files,'source_inputs':spec['inputs'],'qualified_GitHub_revision':os.environ['GITHUB_SHA'],'qualified_run_id':os.environ['GITHUB_RUN_ID'],'full_ABI_application_image_or_deployment_acceptance':False}
    (output/'qualification.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print(json.dumps({'qualified_runtime_candidate':str(output),'package_sha256':row['sha256'],'payload_files_verified':len(files),'application_image_qualified':False}))
if __name__=='__main__':main()
