"""Owned hosted baseline/candidate transport evidence; not full SDK qualification."""
import datetime,hashlib,json,os,pathlib,shutil,subprocess,sys,tempfile
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
owner=pathlib.Path.cwd();out=owner/'pydantic-transport-evidence';out.mkdir()
base='72e4d0be19b63d2bf0950b78fc6d1f33d4429e05';service='services/pydantic-ai-runner/'
def save(name,value):(out/name).write_text(json.dumps(value,indent=2)+'\n',encoding='utf-8')
def git(*args):return subprocess.check_output(['git',*args])
def snapshot():
    result={}
    for item in git('ls-files','-s','-z').split(b'\0'):
        if not item:continue
        left,name=item.split(b'\t');mode,blob,stage=left.decode().split();assert stage=='0';path=owner/name.decode();data=os.readlink(path).encode() if mode=='120000' else path.read_bytes();assert hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()==blob
        result[name.decode()]={'mode':mode,'blob':blob,'sha256':hashlib.sha256(data).hexdigest()}
    return {'head':git('rev-parse','HEAD').decode().strip(),'tree':git('rev-parse','HEAD^{tree}').decode().strip(),'payloads':result}
before=snapshot();save('owner-source-before.json',before)
original_app=git('show',base+':'+service+'app.py');original_case=git('show',base+':'+service+'test_app.py')
assert hashlib.sha256(original_app).hexdigest()=='0e6448ab1418f91b23cf605bb7613897bb4277c2d0502372f8d565e47d666d81'
assert hashlib.sha256(original_case).hexdigest()=='a05bebf80c888a614f6f4d7d2da43e5ff18379a7d0bdae678dec5c8981110565'
assert (owner/service/'test_app.py').read_bytes()==original_case
original_paths=[n for n in before['payloads'] if n.startswith(service)]
records=[];temporary=pathlib.Path(tempfile.mkdtemp(prefix='owned-pydantic-transport-'))
runner="import json,unittest; from pathlib import Path; suite=unittest.defaultTestLoader.loadTestsFromName('test_transport_security'); result=unittest.TextTestRunner(verbosity=2).run(suite); Path('owned-case-results.json').write_text(json.dumps({'tests':result.testsRun,'failures':[case.id() for case,_ in result.failures],'errors':[case.id() for case,_ in result.errors],'skipped':[case.id() for case,_ in result.skipped]})); raise SystemExit(not result.wasSuccessful())"
for profile in ['baseline','candidate']:
    target=temporary/profile;shutil.copytree(owner/service,target)
    if profile=='baseline':(target/'app.py').write_bytes(original_app)
    source_before={n:hashlib.sha256((target/pathlib.PurePosixPath(n).name).read_bytes()).hexdigest() for n in original_paths}
    save(profile+'-service-source-before.json',source_before)
    commands=[]
    for label,args in [('original-app-cases',[sys.executable,'-W','error::ResourceWarning','-m','unittest','-v','test_app']),('owned-real-transport-cases',[sys.executable,'-W','error::ResourceWarning','-c',runner])]:
        with (out/(profile+'-'+label+'.log')).open('wb') as stream:process=subprocess.run(args,cwd=target,stdout=stream,stderr=subprocess.STDOUT,timeout=180)
        commands.append({'label':label,'argv':args,'exit_code':process.returncode})
    source_after={n:hashlib.sha256((target/pathlib.PurePosixPath(n).name).read_bytes()).hexdigest() for n in original_paths};assert source_before==source_after
    save(profile+'-service-source-after.json',source_after)
    cases=json.loads((target/'owned-case-results.json').read_bytes());observations=json.loads((target/'owned-transport-observations.json').read_bytes())
    save(profile+'-owned-case-results.json',cases);save(profile+'-actual-loopback-observations.json',observations)
    records.append({'profile':profile,'commands':commands,'cases':cases,'observations':observations,'service_source_payloads_unchanged':True})
after=snapshot();assert before==after;save('owner-source-after.json',after)
record={'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'baseline_commit':base,'actual_python':sys.version,'original_case_sha256':hashlib.sha256(original_case).hexdigest(),'owned_case_sha256':hashlib.sha256((owner/service/'test_transport_security.py').read_bytes()).hexdigest(),'owner_payloads':len(before['payloads']),'profiles':records,'all_original_app_cases_helpers_fixtures_commands_unchanged':True,'all_publisher_SDK_cases_helpers_commands_unchanged':True,'full_SDK_or_all_runtime_versions_qualified':False,'no_live_provider_credentials_data_or_deployment_used':True}
save('qualification-summary.json',record)
for r in records:assert r['commands'][0]['exit_code']==0 and r['cases']['tests']==10 and not r['cases']['errors'] and not r['cases']['skipped']
baseline,candidate=records
assert baseline['commands'][1]['exit_code']!=0 and len(baseline['cases']['failures'])>=7
assert any(r['label'].startswith('probe-') and r['label'].endswith('-target') and any(q['fixture_authorization'] for q in r['requests']) for r in baseline['observations'])
assert any(r['label'].startswith('completion-') and r['label'].endswith('-target') and any(q['private_prompt_in_body'] for q in r['requests']) for r in baseline['observations'])
assert candidate['commands'][1]['exit_code']==0 and candidate['cases']['failures']==[]
assert all(not r['requests'] for r in candidate['observations'] if r['label'].endswith('-target'))
