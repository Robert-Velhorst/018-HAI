"""Owned source-bound counterfactual CLI tests; inherited partial graph is unqualified."""
import hashlib,io,json,os,pathlib,re,subprocess,tarfile,tempfile
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
owner=pathlib.Path.cwd();out=owner/'garak-transport-evidence';out.mkdir();base='188b695e18c2998d2a1635e395b6619d6c062303'
run_key=os.environ['GITHUB_RUN_ID']+'-'+os.environ['GITHUB_RUN_ATTEMPT'];assert re.fullmatch(r'\d+-\d+',run_key)
service='services/garak-runner';native='services/native-python-runtime';temporary=pathlib.Path(tempfile.mkdtemp(prefix='owned-garak-boundary-'));commands=[];containers=[];images=[]
def save(name,value):(out/name).write_text(json.dumps(value,indent=2)+'\n',encoding='utf-8')
def git(*args):return subprocess.check_output(['git',*args])
def snapshot():
    rows={}
    for item in git('ls-files','-s','-z').split(b'\0'):
        if not item:continue
        left,name=item.split(b'\t');mode,blob,stage=left.decode().split();assert stage=='0';path=owner/name.decode();raw=os.readlink(path).encode() if mode=='120000' else path.read_bytes();assert hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest()==blob
        rows[name.decode()]={'mode':mode,'blob':blob,'sha256':hashlib.sha256(raw).hexdigest()}
    return {'head':git('rev-parse','HEAD').decode().strip(),'tree':git('rev-parse','HEAD^{tree}').decode().strip(),'payloads':rows}
def run(label,args,cwd=owner,timeout=1800):
    with (out/(label+'.log')).open('wb') as stream:result=subprocess.run(args,cwd=cwd,stdout=stream,stderr=subprocess.STDOUT,timeout=timeout)
    commands.append({'label':label,'argv':args,'exit_code':result.returncode});save('commands.json',commands);return result.returncode
before=snapshot();save('owner-source-before.json',before)
old=temporary/'baseline';old.mkdir()
with tarfile.open(fileobj=io.BytesIO(git('archive','--format=tar',base,service,native))) as archive:
    for member in archive.getmembers():
        parts=pathlib.PurePosixPath(member.name).parts;assert parts and not member.name.startswith('/') and '..' not in parts
        destination=old.joinpath(*parts)
        if member.isdir():destination.mkdir(parents=True,exist_ok=True);continue
        assert member.isfile();destination.parent.mkdir(parents=True,exist_ok=True);destination.write_bytes(archive.extractfile(member).read());destination.chmod(member.mode)
assert hashlib.sha256((old/service/'test_app.py').read_bytes()).hexdigest()=='253a27df0b55b235df3d0ff6c66a5af03e7ed4dd38095bd5327f229fc12e3509'
assert (old/service/'test_app.py').read_bytes()==(owner/service/'test_app.py').read_bytes()
case=(owner/service/'transport_security.py').read_bytes();records=[]
case_runner="import json,unittest; suite=unittest.defaultTestLoader.loadTestsFromName('transport_security'); result=unittest.TextTestRunner(verbosity=2).run(suite); print('OWNED_CASE_RESULTS='+json.dumps({'tests':result.testsRun,'failures':[case.id() for case,_ in result.failures],'errors':[case.id() for case,_ in result.errors],'skipped':[case.id() for case,_ in result.skipped]})); raise SystemExit(not result.wasSuccessful())"
try:
    for profile,source in [('baseline',old),('candidate',owner)]:
        image='owned-garak-'+profile+':'+run_key;wrapper='owned-garak-'+profile+'-cases:'+run_key
        assert subprocess.run(['docker','image','inspect',image],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
        assert run(profile+'-build-original-recipe',['docker','build','--label','org.hai.transport='+run_key,'--build-context','hai-native-source='+str(source/native),'--file','Dockerfile','--tag',image,'.'],cwd=source/service)==0
        images.append(image)
        context=temporary/(profile+'-cases');context.mkdir();(context/'transport_security.py').write_bytes(case);(context/'Dockerfile').write_text('FROM '+image+'\nCOPY transport_security.py /app/transport_security.py\n',encoding='utf-8')
        assert run(profile+'-build-owned-case-wrapper',['docker','build','--label','org.hai.transport='+run_key,'--tag',wrapper,str(context)])==0;images.append(wrapper)
        exits={}
        for label,args in [('original-app-cases',['python','-W','error::ResourceWarning','-m','unittest','-v','test_app']),('owned-actual-cli-cases',['python','-W','error::ResourceWarning','-c',case_runner])]:
            name='owned-garak-'+profile+'-'+label+'-'+run_key
            assert subprocess.run(['docker','container','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
            containers.append(name)
            exits[label]=run(profile+'-'+label,['docker','run','--name',name,'--label','org.hai.transport='+run_key,'--network','none','--read-only','--tmpfs','/tmp:rw,nosuid,size=256m,mode=1777','--cap-drop','ALL','--security-opt','no-new-privileges','--memory','2g','--cpus','2',wrapper,*args],timeout=900)
            if profile=='candidate' and label=='original-app-cases':
                assert run('candidate-copy-additive-vendor-source-proof',['docker','cp',name+':/usr/share/doc/hai-garak-local-generator/source-identity.json',str(out/'candidate-installed-plugin-source-identity.json')])==0
        log=(out/(profile+'-owned-actual-cli-cases.log')).read_text();case_rows=[line.removeprefix('OWNED_CASE_RESULTS=') for line in log.splitlines() if line.startswith('OWNED_CASE_RESULTS=')];observation_rows=[line.removeprefix('OWNED_GARAK_OBSERVATIONS=') for line in log.splitlines() if line.startswith('OWNED_GARAK_OBSERVATIONS=')];assert len(case_rows)==len(observation_rows)==1
        cases=json.loads(case_rows[0]);observations=json.loads(observation_rows[0]);save(profile+'-owned-case-results.json',cases);save(profile+'-actual-transport-observations.json',observations);records.append({'profile':profile,'commands':exits,'cases':cases,'actual':observations})
finally:
    for name in containers:
        inspected=subprocess.run(['docker','container','inspect','--format','{{json .Config.Labels}}',name],capture_output=True,text=True)
        if inspected.returncode==0:
            assert json.loads(inspected.stdout)['org.hai.transport']==run_key
            run('cleanup-'+name,['docker','rm','--force',name],timeout=60)
    for image in reversed(images):
        labels=json.loads(subprocess.check_output(['docker','image','inspect','--format','{{json .Config.Labels}}',image]));assert labels['org.hai.transport']==run_key
        run('cleanup-'+image.replace(':','-'),['docker','image','rm',image],timeout=60)
after=snapshot();assert before==after;save('owner-source-after.json',after)
summary={'baseline_commit':base,'owner_payloads':len(before['payloads']),'owned_case_sha256':hashlib.sha256(case).hexdigest(),'profiles':records,'commands':commands,'all_original_app_and_vendor_SDK_cases_helpers_commands_unchanged':True,'original_partial_profile_and_upstream_OpenAI_constraint_unqualified':True,'whole_original_SDK_graph_and_current_native_images_unqualified':True,'no_live_provider_credentials_data_or_deployment':True};save('qualification-summary.json',summary)
assert len(records)==2
for r in records:assert r['commands']['original-app-cases']==0 and r['cases']['tests']==10 and not r['cases']['errors'] and not r['cases']['skipped'] and r['actual']['garak']=='0.17.1' and r['actual']['openai']=='3.37.0'
baseline,candidate=records;assert baseline['commands']['owned-actual-cli-cases']!=0 and len(baseline['cases']['failures'])>=7
assert any(r['label'].startswith('probe-') and r['label'].endswith('-target') and any(q['fixture_authorization'] for q in r['requests']) for r in baseline['actual']['observations'])
assert any(r['label'].startswith('cli-') and r['label'].endswith('-target') and any(q['contains_messages'] for q in r['requests']) for r in baseline['actual']['observations'])
assert candidate['commands']['owned-actual-cli-cases']==0 and not candidate['cases']['failures']
assert all(not r['requests'] for r in candidate['actual']['observations'] if r['label'].endswith('-target'))
