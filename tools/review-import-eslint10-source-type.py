"""Owned hosted regression proof; full upstream SDK acceptance remains separate."""
from pathlib import Path,PureWindowsPath
import hashlib,json,os,re,shutil,subprocess,sys

assert os.environ.get('GITHUB_ACTIONS')=='true'
assert os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
root=Path(__file__).resolve().parents[1]
assert root==Path.cwd()
out=root/'review-output/import-eslint10-source-type';out.mkdir(parents=True)
sdk=out/'publisher-source';harness=out/'harness';harness.mkdir()
commit='01c9eb04331d2efa8d63f2d7f4bfec3bc44c94f3'
tree='1db645f82530ef11adff0d8fb17824144b7f7130'
relative='src/core/sourceType.js'
commands=[]
sha=lambda data:hashlib.sha256(data).hexdigest()

def command(label,args,cwd=root,env=None):
    with (out/(label+'.log')).open('wb') as stream:
        result=subprocess.run(args,cwd=cwd,env=env,stdout=stream,stderr=subprocess.STDOUT,check=False)
    commands.append({'label':label,'command':args,'cwd':str(cwd.relative_to(root)) or '.','returncode':result.returncode})
    return result.returncode

def git(*args):return subprocess.check_output(['git','-C',str(sdk),*args])

def read_payload(folder,name,mode):
    path=folder/name
    if mode=='120000' and path.is_symlink():
        target=os.readlink(path)
        # Windows stores native link separators differently from Git's POSIX blob.
        # Retain the actual target as evidence and still require the exact Git hash.
        if os.name=='nt':
            assert not PureWindowsPath(target).is_absolute(),name
            canonical=target.replace('\\','/')
        else:canonical=target
        return canonical.encode(),{'native_target':target,'canonical_Git_target':canonical}
    assert path.resolve().is_relative_to(folder.resolve()),name
    return path.read_bytes(),None

def snapshot(folder):
    result={};links={};native_links={}
    rows=subprocess.check_output(['git','-C',str(folder),'ls-tree','-r','-z','HEAD'])
    for row in rows.split(b'\0'):
        if not row:continue
        meta,name=row.split(b'\t',1);mode,kind,oid=meta.decode().split();name=name.decode()
        assert name and not name.startswith('/') and '..' not in name.split('/')
        if kind=='commit':links[name]={'mode':mode,'commit':oid};continue
        assert kind=='blob' and mode in {'100644','100755','120000'}
        data,native=read_payload(folder,name,mode)
        if native is not None:native_links[name]=native
        actual=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
        assert actual==oid,{'path':name,'expected_blob':oid,'actual_blob':actual,'native_symlink':native}
        result[name]={'mode':mode,'blob':oid,'sha256':sha(data)}
    return {'head':subprocess.check_output(['git','-C',str(folder),'rev-parse','HEAD']).decode().strip(),'tree':subprocess.check_output(['git','-C',str(folder),'rev-parse','HEAD^{tree}']).decode().strip(),'payloads':result,'unexpanded_gitlinks':links,'native_symlink_targets':native_links}

project=snapshot(root)
(out/'project-source-before.json').write_text(json.dumps(project,indent=2)+'\n')
summary={'head':project['head'],'complete_original_publisher_SDK_qualified':False,'application_adoption':False,'peer_range_widened_or_waived':False,'original_case_helper_fixture_assertion_selector_timeout_or_command_changes':False}
try:
    metadata=json.loads(subprocess.check_output(['gh','api','repos/import-js/eslint-plugin-import/git/commits/'+commit]))
    assert metadata['sha']==commit and metadata['tree']['sha']==tree and metadata['verification']['verified']
    (out/'publisher-verified-commit.json').write_text(json.dumps(metadata,indent=2)+'\n')
    assert command('source-clone',['git','clone','--no-checkout','https://github.com/import-js/eslint-plugin-import.git',str(sdk)])==0
    assert command('source-checkout',['git','-C',str(sdk),'checkout','--detach',commit])==0
    before=snapshot(sdk);assert before['head']==commit and before['tree']==tree and not before['unexpanded_gitlinks']
    (out/'publisher-source-before.json').write_text(json.dumps(before,indent=2)+'\n')
    original=(sdk/relative).read_bytes()
    assert hashlib.sha1(b'blob '+str(len(original)).encode()+b'\0'+original).hexdigest()=='5ff92edc97ec37ca10e5e9eb5943b872ae22bc16'
    old=b"if ('sourceType' in context.parserOptions) {"
    new=b"if (context.parserOptions && 'sourceType' in context.parserOptions) {"
    assert original.count(old)==1
    patched=original.replace(old,new)
    (sdk/relative).write_bytes(patched)
    (out/'original-sourceType.js').write_bytes(original)
    (out/'proposed-sourceType.js').write_bytes(patched)
    (harness/'baseline-sourceType.mjs').write_bytes(original)
    (harness/'port-sourceType.mjs').write_bytes(patched)
    (out/'publisher-LICENSE').write_bytes((sdk/'LICENSE').read_bytes())
    (out/'original-publisher-package.json').write_bytes((sdk/'package.json').read_bytes())
    (harness/'package.json').write_text(json.dumps({'name':'owned-import-eslint10-component-review','version':'1.0.0','private':True,'type':'module','dependencies':{'eslint':'10.13.0'}},indent=2)+'\n')
    case_bytes=(root/'tools/review-import-eslint10-source-type.test.mjs').read_bytes()
    (harness/'source-type.test.mjs').write_bytes(case_bytes)
    wrapper=Path(shutil.which('npm.cmd') or shutil.which('npm'))
    npm_cli=wrapper.parent/'node_modules/npm/bin/npm-cli.js' if os.name=='nt' else wrapper.resolve()
    assert npm_cli.is_file()
    npm=['node',str(npm_cli)]
    summary['actual_node_version']=subprocess.check_output(['node','--version']).decode().strip()
    summary['actual_npm_version']=subprocess.check_output([*npm,'--version']).decode().strip()
    assert summary['actual_node_version']=='v26.12.1' and summary['actual_npm_version']=='12.2.1'
    effective={}
    for setting,expected in [('strict-peer-deps','true'),('engine-strict','true'),('force','false'),('legacy-peer-deps','false')]:
        value=subprocess.check_output([*npm,'config','get',setting],cwd=harness).decode().strip();assert value==expected,(setting,value);effective[setting]=value
    summary['effective_npm_settings']=effective
    assert command('normal-lock-resolution',[*npm,'install','--package-lock-only','--registry=https://registry.npmjs.org/'],harness)==0
    assert command('normal-complete-install',[*npm,'ci','--registry=https://registry.npmjs.org/'],harness)==0
    graph_exit=command('complete-installed-graph',[*npm,'ls','--all','--json'],harness)
    audit_exit=command('full-unfiltered-audit',[*npm,'audit','--json','--registry=https://registry.npmjs.org/'],harness)
    stages={}
    for stage in ['baseline','port']:
        environment=os.environ.copy();environment['IMPORT_SOURCE_TYPE_STAGE']=stage
        result=command(stage+'-actual-eslint10-cases',['node','--test','--test-reporter=tap','source-type.test.mjs'],harness,environment)
        log=(out/(stage+'-actual-eslint10-cases.log')).read_text()
        expected=(3,3) if stage=='baseline' else (6,0)
        assert re.search(r'^# tests 6\s*$',log,re.M) and re.search(r'^# pass '+str(expected[0])+r'\s*$',log,re.M) and re.search(r'^# fail '+str(expected[1])+r'\s*$',log,re.M)
        assert re.search(r'^# skipped 0\s*$',log,re.M) and result==(1 if stage=='baseline' else 0)
        if stage=='baseline':assert "Cannot use 'in' operator" in log and 'sourceType' in log
        stages[stage]={'returncode':result,'cases':6,'passed':expected[0],'failed':expected[1],'skipped':0,'case_file_sha256':sha(case_bytes),'log_sha256':sha((out/(stage+'-actual-eslint10-cases.log')).read_bytes())}
    assert graph_exit==0 and audit_exit==0
    audit=json.loads((out/'full-unfiltered-audit.log').read_text());assert audit['metadata']['vulnerabilities']['total']==0
    summary.update(component_regression_qualified=True,stages=stages,full_audit=audit['metadata']['vulnerabilities'],installed_graph_exit=graph_exit)
except Exception as error:
    summary.update(component_regression_qualified=False,failure=type(error).__name__+': '+str(error)[:400])
finally:
    if 'before' in locals():
        preserved={};native_after={}
        for name,row in before['payloads'].items():
            if name==relative:continue
            current,native=read_payload(sdk,name,row['mode'])
            if native is not None:native_after[name]=native
            assert sha(current)==row['sha256'],name;preserved[name]=row
        assert native_after==before['native_symlink_targets']
        (out/'publisher-source-after.json').write_text(json.dumps({'head':commit,'tree':tree,'all_other_payloads_unchanged':preserved,'only_production_path_changed':relative,'proposed_source_sha256':sha((sdk/relative).read_bytes()),'native_symlink_targets_unchanged':native_after},indent=2)+'\n')
    for name,row in project['payloads'].items():
        data,native=read_payload(root,name,row['mode'])
        assert sha(data)==row['sha256'],name
    (out/'project-source-after.json').write_text(json.dumps(project,indent=2)+'\n')
    (out/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
    (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary))
sys.exit(0 if summary.get('component_regression_qualified') else 1)
