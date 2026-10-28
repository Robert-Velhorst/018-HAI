"""Hosted original SDK commands against a reviewable native-API production port."""
import datetime,hashlib,json,os,pathlib,re,subprocess,sys,urllib.request
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
owner=pathlib.Path(__file__).resolve().parents[2];sdk=owner.parent/'fetch-blob-publisher';out=owner/'FETCH_BLOB_NATIVE_SDK_EVIDENCE';out.mkdir(exist_ok=True)
desc=json.loads((owner/'.github/qualification/fetch-blob-native-domexception.json').read_bytes())
def git(root,*args):return subprocess.check_output(['git','-C',str(root),*args])
def snapshot(root,allow_changes=()):
    rows={}
    for row in git(root,'ls-tree','-rz','HEAD').split(b'\0'):
        if not row:continue
        h,n=row.split(b'\t');mode,kind,blob=h.decode().split();assert kind=='blob';name=os.fsdecode(n);p=root/name;b=os.readlink(p).encode() if mode=='120000' else p.read_bytes();oid=hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()
        if name not in allow_changes:assert oid==blob
        rows[name]={'mode':mode,'index_blob':blob,'physical_blob':oid,'sha256':hashlib.sha256(b).hexdigest()}
    return rows
assert git(sdk,'rev-parse','HEAD').decode().strip()==desc['upstream_commit']
assert subprocess.check_output(['node','--version']).decode().strip()=='v26.12.1'
assert subprocess.check_output(['npm','--version']).decode().strip()=='12.2.1'
original=snapshot(sdk);owner_before=snapshot(owner);assert len(original)==15
for r in desc['original_payloads']:assert original[r['path']]['sha256']==r['sha256'] and original[r['path']]['index_blob']==r['blob']
(out/'original-source.json').write_text(json.dumps(original,indent=2));(out/'owner-before.json').write_text(json.dumps(owner_before,indent=2))
for n in ['from.js','package.json']:(out/('original-'+n)).write_bytes((sdk/n).read_bytes())
(sdk/'from.js').write_bytes(desc['candidate_from_js'].encode());(sdk/'package.json').write_bytes((json.dumps(desc['candidate_package'],indent=2)+'\n').encode())
candidate=snapshot(sdk,('from.js','package.json'));assert [n for n in original if original[n]!=candidate[n]]==['from.js','package.json']
(out/'candidate-before.json').write_text(json.dumps(candidate,indent=2));(out/'candidate-from.js').write_bytes((sdk/'from.js').read_bytes());(out/'candidate-package.json').write_bytes((sdk/'package.json').read_bytes())
record={'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'head':git(owner,'rev-parse','HEAD').decode().strip(),'tree':git(owner,'rev-parse','HEAD^{tree}').decode().strip(),'SDK_commit':desc['upstream_commit'],'private_port':desc['candidate_package']['version'],'node':'v26.12.1','npm':'12.2.1','steps':[],'original_case_helper_assertion_fixture_selector_timeout_or_command_changes':False,'known_original_ignored_cases':desc['inherited_original_harness_ignored_case_names'],'complete_original_harness_all_cases_or_job_matrix_qualified':False,'actual_application_parent_graph_native_saved_image_database_or_live_provider_qualified':False,'application_graph_adoption':False}
def run(name,argv):
    log=out/(name+'.log')
    with log.open('wb') as stream:r=subprocess.run(argv,cwd=sdk,stdout=stream,stderr=subprocess.STDOUT,timeout=300)
    record['steps'].append({'name':name,'argv':argv,'returncode':r.returncode,'log':log.name,'sha256':hashlib.sha256(log.read_bytes()).hexdigest()});return r.returncode
try:
    cache=sdk/'test/.cache';cache.mkdir(exist_ok=True);inputs=[]
    record['actual_original_WPT_URL_inputs']=inputs
    for r in desc['WPT_original_URL_inputs']:
        url=r['origin'];assert url=='https://wpt.live/'+r['path']
        assert url in {'https://wpt.live/resources/testharness.js','https://wpt.live/FileAPI/file/File-constructor.any.js','https://wpt.live/FileAPI/blob/Blob-constructor.any.js','https://wpt.live/FileAPI/blob/Blob-array-buffer.any.js','https://wpt.live/FileAPI/blob/Blob-slice-overflow.any.js','https://wpt.live/FileAPI/blob/Blob-slice.any.js','https://wpt.live/FileAPI/blob/Blob-stream.any.js','https://wpt.live/FileAPI/blob/Blob-text.any.js'}
        with urllib.request.urlopen(url,timeout=30) as response:b=response.read(4*1024**2+1)
        assert len(b)<=4*1024**2 and hashlib.sha256(b).hexdigest()==r['sha256']
        assert hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()==r['git_blob']
        p=cache/url.encode().hex();p.write_bytes(b);saved=out/'WPT-inputs'/r['path'];saved.parent.mkdir(parents=True,exist_ok=True);saved.write_bytes(b);inputs.append({'url':url,'sha256':r['sha256'],'git_blob':r['git_blob'],'cache_filename':p.name})
    record['actual_original_WPT_URL_inputs']=inputs
    assert run('effective-engine-strict',['npm','config','get','engine-strict'])==0
    assert run('effective-strict-peer-deps',['npm','config','get','strict-peer-deps'])==0
    assert (out/'effective-engine-strict.log').read_text().strip()=='true' and (out/'effective-strict-peer-deps.log').read_text().strip()=='true'
    assert run('original-normal-install',['npm','install'])==0
    assert run('complete-actual-SDK-graph',['npm','ls','--all','--json'])==0
    graph=json.loads((out/'complete-actual-SDK-graph.log').read_bytes());installed=[]
    def walk(deps):
        for n,r in deps.items():
            if r.get('version'):
                assert not r.get('invalid') and not r.get('missing') and not r.get('extraneous')
                assert re.fullmatch(r'\d+(?:\.\d+)*(?:\+[A-Za-z0-9.-]+)?',r['version'])
                installed.append({'name':n,'version':r['version']})
            walk(r.get('dependencies',{}))
    walk(graph.get('dependencies',{}));record['actual_installed_SDK_graph']=installed
    assert not any(r['name']=='node-domexception' for r in installed)
    for n,v in desc['candidate_package']['devDependencies'].items():assert graph['dependencies'][n]['version']==v
    for name,argv in [('original-test',['npm','test']),('original-coverage-report',['npm','run','report','--','--colors']),('original-declaration-build',['npm','run','prepublishOnly'])]:
        code=run(name,argv)
        text=re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]','',(out/(name+'.log')).read_text(errors='replace'))
        outcomes=[{'status':'passed' if m[0]=='√' else 'failed','name':m[1]} for m in re.findall(r'^\s*([√×])\s+(.+)$',text,re.M)]
        record[name]={'returncode':code,'reported_cases':outcomes,'passed':sum(r['status']=='passed' for r in outcomes),'failed':sum(r['status']=='failed' for r in outcomes),'known_original_unreported_cases':len(record['known_original_ignored_cases'])}
        assert code==0
        if name!='original-declaration-build':assert outcomes and not any(r['status']=='failed' for r in outcomes)
    record['original_selected_test_report_and_declaration_commands_succeeded']=True
    record['separate_audit_returncode']=run('complete-actual-SDK-audit',['npm','audit','--json'])
    # Preserve the publisher's package-lock=false setting even if it prevents
    # this independent audit; never treat ENOLOCK as a clean audit.
    assert run('private-port-pack',['npm','pack','--json','--pack-destination',str(out)])==0
    record['private_pack_created']=True
    assert record['separate_audit_returncode']==0,'Independent SDK audit failed or lacks a lock; no waiver permitted'
except BaseException as error:
    record['failure']={'type':type(error).__name__,'message':str(error)}
    raise
finally:
    after=snapshot(sdk,('from.js','package.json'));owner_after=snapshot(owner)
    (out/'candidate-after.json').write_text(json.dumps(after,indent=2));(out/'owner-after.json').write_text(json.dumps(owner_after,indent=2))
    record['all_other_original_SDK_payloads_unchanged']=len(original)-2
    record['original_case_files_unchanged']=all(original[n]==after[n] for n in original if n.startswith('test/'))
    record['candidate_tracked_bytes_unchanged_during_original_commands']=candidate==after
    record['owner_tracked_bytes_unchanged_during_original_commands']=owner_before==owner_after
    record['WPT_cache_files_unchanged']=set(p.name for p in cache.iterdir())=={r['cache_filename'] for r in record.get('actual_original_WPT_URL_inputs',[])} and all(hashlib.sha256((cache/r['cache_filename']).read_bytes()).hexdigest()==r['sha256'] for r in record.get('actual_original_WPT_URL_inputs',[]))
    (out/'receipt.json').write_text(json.dumps(record,indent=2));assert candidate==after and owner_before==owner_after and record['original_case_files_unchanged'] and record['WPT_cache_files_unchanged']
