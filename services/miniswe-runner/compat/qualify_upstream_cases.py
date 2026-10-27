"""Qualify pinned unchanged vendor cases in hosted public disposable SDKs."""
from pathlib import Path,PurePosixPath
from concurrent.futures import ThreadPoolExecutor
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import urllib.request
from urllib.parse import urlsplit


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--sdk',required=True)
    args=parser.parse_args()
    expected='python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51'
    run_id,attempt=os.environ.get('GITHUB_RUN_ID',''),os.environ.get('GITHUB_RUN_ATTEMPT','')
    if os.environ.get('GITHUB_ACTIONS')!='true' or sys.platform!='linux' or args.sdk!=expected or not run_id.isdigit() or not attempt.isdigit():
        raise RuntimeError('Existing case SDK execution is restricted to an identified hosted job')
    output=args.output.resolve(strict=True)
    qualification=json.loads((output/'qualification/qualification.json').read_text(encoding='utf-8'))
    if not qualification['qualified']:raise ValueError('Real SDK import qualification is required first')
    compat=Path(__file__).resolve().parent
    sources=output/'upstream-cases';sources.mkdir(exist_ok=False)
    proof=output/'upstream-case-results';proof.mkdir(exist_ok=False);proof.chmod(0o777)
    case_tools=output/'case-tools';case_tools.mkdir(exist_ok=False);case_tools.chmod(0o777)
    inputs=output/'inputs'
    for name in ['upstream-case-specs.json','run_upstream_cases.py','requirements.sdk-cases.in']:
        (inputs/name).write_bytes((compat/name).read_bytes())
    specs=json.loads((inputs/'upstream-case-specs.json').read_text(encoding='utf-8'))
    if {s['name'] for s in specs}!={'fastuuid','datasets','botocore'}:raise ValueError('Unexpected case suites')
    jobs=[]
    for suite in specs:
        (sources/suite['name']).mkdir()
        for item in suite['files']:jobs.append((suite,item))

    def prepare(job):
        suite,item=job
        path=PurePosixPath(item['path'])
        if path.is_absolute() or '..' in path.parts or '\\' in item['path']:
            raise ValueError('Case path escapes its source root')
        if 'url' in item:
            url=urlsplit(item['url'])
            prefix='/'+suite['repository']+'/'+suite['source_commit']+'/'
            if url.scheme!='https' or url.hostname!='raw.githubusercontent.com' or url.username or url.password or not url.path.startswith(prefix):
                raise ValueError('Case source must use its pinned public repository commit')
            raw=urllib.request.urlopen(item['url'],timeout=40).read(1024*1024+1)
        else:
            raw=(output/'fastuuid-build/source'/item['prepared_fastuuid_path']).read_bytes()
        if len(raw)!=item['bytes'] or hashlib.sha256(raw).hexdigest()!=item['sha256']:
            raise ValueError('Pinned upstream source/helper/license differs')
        destination=sources/suite['name']/item['path']
        destination.parent.mkdir(parents=True,exist_ok=True);destination.write_bytes(raw)
        return {'suite':suite['name'],'path':item['path'],'sha256':item['sha256']}
    with ThreadPoolExecutor(max_workers=4) as pool:manifest=list(pool.map(prepare,jobs))
    (proof/'source-records.json').write_text(json.dumps(manifest,indent=2)+'\n',encoding='utf-8')
    owner='upstream-cases-'+run_id+'-'+attempt

    def run_owned(command,phase,seconds):
        cidfile=output/('owned-upstream-'+phase+'.cid')
        command=command[:3]+['--cidfile',str(cidfile),'--label','io.hai.sdk-owner='+owner]+command[3:]
        try:return subprocess.run(command,capture_output=True,check=False,timeout=seconds)
        except subprocess.TimeoutExpired as error:
            if cidfile.is_file():
                cid=cidfile.read_text().strip()
                if re.fullmatch(r'[0-9a-f]{64}',cid):
                    label=subprocess.run(['docker','inspect','--format','{{index .Config.Labels "io.hai.sdk-owner"}}',cid],capture_output=True,text=True,timeout=10)
                    if label.returncode==0 and label.stdout.strip()==owner:
                        subprocess.run(['docker','rm','--force',cid],capture_output=True,check=True,timeout=30)
            return subprocess.CompletedProcess(command,124,error.stdout or b'',error.stderr or b'')

    common=['docker','run','--rm','--read-only','--user','10001:10001','--cap-drop','ALL',
            '--security-opt','no-new-privileges:true','--memory','2g','--cpus','2','--pids-limit','256',
            '--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=768m,uid=10001,gid=10001,mode=0700',
            '--env','HOME=/tmp','--env','PYTHONDONTWRITEBYTECODE=1']
    native_spec=json.loads((compat/'fastuuid-port-spec.json').read_text(encoding='utf-8'))
    tools=common+['--env','UV_PYTHON_DOWNLOADS=never','--env','UV_CACHE_DIR=/cache',
                  '--env','UV_CONCURRENT_BUILDS=1','--env','UV_CONCURRENT_DOWNLOADS=2',
                  '--env','UV_CONCURRENT_INSTALLS=1','--env','TMPDIR=/case-build',
                  '--env','CC=/usr/local/bin/gcc','--env','CXX=/usr/local/bin/g++',
                  '--env','LD_LIBRARY_PATH=/python/lib:/usr/local/lib64:/usr/local/lib',
                  '--env','PATH=/python/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
                  '--tmpfs','/case-build:rw,exec,nosuid,nodev,size=1g,uid=10001,gid=10001,mode=0700',
                  '--tmpfs','/sdk-tools:rw,exec,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=0700']
    for src,dst,readonly in [(inputs,'/inputs',True),(output/'tools','/tools',True),
                              (output/'outputs','/outputs',False),(output/'cache','/cache',False),
                              (case_tools,'/case-tools',False),
                              (output/'fastuuid-build/python','/python',True)]:
        tools+=['--mount',f'type=bind,src={src},dst={dst}'+(',readonly' if readonly else '')]
    tools+=[native_spec['compiler']['sdk_image'],'sh','-c','''set -eu
test "$("$CC" -dumpfullversion)" = 16.2.0
python -c 'import sys; assert sys.version_info[:3] == (3,15,1)'
"$CC" --version
cp /tools/uv /sdk-tools/uv
chmod 700 /sdk-tools/uv
/sdk-tools/uv pip compile /inputs/requirements.sdk-cases.in --upgrade --generate-hashes --emit-index-url --no-python-downloads --python-version 3.15.1 --python-platform x86_64-unknown-linux-gnu --output-file /outputs/requirements.sdk-cases.lock
/sdk-tools/uv pip install --no-python-downloads --require-hashes --build-constraints /inputs/requirements.sdk-cases.in --target /case-tools -r /outputs/requirements.sdk-cases.lock
''']
    setup=run_owned(tools,'tools',600)
    (proof/'tools-resolution-installation.log').write_bytes(setup.stdout+setup.stderr)
    results=[]
    if setup.returncode==0:
        for suite in specs:
            command=common+['--network','none','--env','HAI_DISPOSABLE_UPSTREAM_CASES=1',
                            '--env','HF_HUB_OFFLINE=1','--env','HF_HUB_DISABLE_TELEMETRY=1',
                            '--env','AWS_EC2_METADATA_DISABLED=true','--env','HYPOTHESIS_PROFILE=ci']
            for src,dst,readonly in [(inputs,'/inputs',True),(output/'site-packages','/installed',True),
                                      (case_tools,'/case-tools',True),(sources/suite['name'],'/upstream',True),
                                      (proof,'/qualification',False)]:
                command+=['--mount',f'type=bind,src={src},dst={dst}'+(',readonly' if readonly else '')]
            command+=[args.sdk,'python','-I','/inputs/run_upstream_cases.py','--suite',suite['name']]
            result=run_owned(command,suite['name'],600)
            (proof/(suite['name']+'-execution.log')).write_bytes(result.stdout+result.stderr)
            path=proof/(suite['name']+'-upstream-cases.json')
            row=json.loads(path.read_text(encoding='utf-8')) if path.exists() else {'suite':suite['name'],'qualified':False}
            row['container_returncode']=result.returncode
            results.append(row)
    receipt={'tools_resolution_installation_returncode':setup.returncode,'suites':results,
             'source_files_verified':len(manifest),'new_application_cases_authored':False,
             'operator_data_credentials_or_SDK_mounted':False,'network_for_case_execution':'none',
             'qualified':setup.returncode==0 and len(results)==3 and all(r.get('qualified') and r['container_returncode']==0 for r in results),
             'full_application_data_or_provider_acceptance':False}
    (proof/'qualification.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(receipt))
    if not receipt['qualified']:
        if setup.returncode:print((setup.stdout+setup.stderr).decode(errors='replace')[-6000:])
        for suite in specs:
            path=proof/(suite['name']+'-execution.log')
            if path.exists():print(path.read_text(encoding='utf-8',errors='replace')[-6000:])
        raise SystemExit(1)


if __name__=='__main__':main()
