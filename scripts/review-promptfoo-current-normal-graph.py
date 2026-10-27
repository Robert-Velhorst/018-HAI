"""Owned hosted-worker experiment; preserve original cases and retain the complete graph/audit."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import quote
from urllib.request import Request, urlopen
import hashlib,json,os,re,subprocess,sys

assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
root=Path.cwd();runner=root/'services/promptfoo-runner';out=Path(os.environ['RUNNER_TEMP'])/'promptfoo-current-normal-graph';out.mkdir()
sha=lambda b:hashlib.sha256(b).hexdigest()
git=lambda *a:subprocess.check_output(['git',*a])
head=git('rev-parse','HEAD').decode().strip();tree=git('rev-parse','HEAD^{tree}').decode().strip()
source={}
for row in git('ls-tree','-r','-z','HEAD').split(b'\0'):
 if not row:continue
 meta,name=row.split(b'\t',1);mode,kind,blob=meta.decode().split();name=name.decode();assert kind=='blob' and mode in {'100644','100755'}
 data=(root/name).read_bytes();assert hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()==blob,name
 source[name]={'blob':blob,'sha256':sha(data)}
(out/'source-before.json').write_text(json.dumps({'head':head,'tree':tree,'payloads':source},indent=2)+'\n')
allowed={'services/promptfoo-runner/package.json','services/promptfoo-runner/package-lock.json','services/promptfoo-runner/app.mjs','services/promptfoo-runner/Dockerfile'}
for name in ['package.json','package-lock.json','app.mjs','Dockerfile','test_app.mjs','test_cli_compatibility.mjs']:
 (out/('before-'+name)).write_bytes((runner/name).read_bytes())
commands=[];summary={'head':head,'full_qualification':False,'original_cases_fixtures_helpers_assertions_commands_or_timeouts_changed':False,'local_execution':False}

def run(label,command,cwd=runner):
 with (out/(label+'.log')).open('wb') as stream:
  result=subprocess.run(command,cwd=cwd,stdout=stream,stderr=subprocess.STDOUT,check=False)
 commands.append({'label':label,'command':command,'cwd':str(cwd.relative_to(root)) or '.','returncode':result.returncode})
 return result.returncode

def public(name):
 try:
  request=Request('https://registry.npmjs.org/'+quote(name,safe=''),headers={'Accept':'application/vnd.npm.install-v1+json'})
  with urlopen(request,timeout=40) as response:raw=response.read(16*1024*1024+1)
  assert len(raw)<=16*1024*1024
  catalog=json.loads(raw);stable=[v for v,m in catalog['versions'].items() if re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)',v) and not m.get('deprecated')]
  version=max(stable,key=lambda v:tuple(map(int,v.split('.'))))
  with urlopen('https://registry.npmjs.org/'+quote(name,safe='')+'/'+version,timeout=40) as response:manifest=json.load(response)
  assert manifest['name']==name and manifest['version']==version
  return {'name':name,'highest_stable':version,'dist_tags':catalog.get('dist-tags',{}),'dependencies':manifest.get('dependencies',{}),'optionalDependencies':manifest.get('optionalDependencies',{}),'peerDependencies':manifest.get('peerDependencies',{}),'engines':manifest.get('engines',{}),'dist':manifest.get('dist',{}),'error':None}
 except Exception as error:return {'name':name,'error':type(error).__name__+': '+str(error)[:180]}

try:
 manifest=json.loads((runner/'package.json').read_text());baseline=json.loads((runner/'package-lock.json').read_text());old_engine=manifest['dependencies']['promptfoo']
 targets=[public(name) for name in sorted(manifest['dependencies'])];assert all(row['error'] is None for row in targets)
 (out/'direct-public-targets.json').write_text(json.dumps(targets,indent=2)+'\n')
 expected=json.loads(json.dumps(manifest));expected.pop('overrides',None)
 assert not any(k in expected for k in ['overrides','resolutions'])
 for row in targets:expected['dependencies'][row['name']]=row['highest_stable']
 private_path=runner/'third_party/get-uri8-basicftp6-private.tgz'
 private_provenance=json.loads((runner/'third_party/get-uri8-basicftp6-provenance.json').read_text())
 assert sha(private_path.read_bytes())==private_provenance['private_package_sha256']
 expected['dependencies']['get-uri']='file:third_party/get-uri8-basicftp6-private.tgz'
 (runner/'package.json').write_text(json.dumps(expected,indent=2)+'\n')
 app=(runner/'app.mjs').read_text();needle="export const ENGINE = 'promptfoo "+old_engine+"';";assert app.count(needle)==1
 app=app.replace(needle,"export const ENGINE = 'promptfoo "+expected['dependencies']['promptfoo']+"';")
 (runner/'app.mjs').write_text(app)
 dockerfile=(runner/'Dockerfile').read_text();marker='COPY package.json package-lock.json ./\n';assert dockerfile.count(marker)==1
 (runner/'Dockerfile').write_text(dockerfile.replace(marker,marker+'COPY third_party ./third_party\n'))
 assert run('normal-resolve',['npm','update','--package-lock-only','--ignore-scripts','--no-audit','--no-fund'])==0
 lock=json.loads((runner/'package-lock.json').read_text());assert lock['packages']['']['dependencies']==expected['dependencies']
 assert json.loads((runner/'package.json').read_text())==expected
 for name in ['package.json','package-lock.json','app.mjs','Dockerfile']:(out/('generated-'+name)).write_bytes((runner/name).read_bytes())
 install_exit=run('complete-install',['npm','ci','--ignore-scripts','--no-audit','--no-fund'])
 if install_exit==0:
  import tarfile,io
  private_members={}
  with tarfile.open(fileobj=io.BytesIO(private_path.read_bytes()),mode='r:gz') as archive:
   for member in archive:
    assert member.isfile() and member.name.startswith('package/')
    name=member.name[len('package/'):];data=archive.extractfile(member).read()
    assert name not in private_members and (runner/'node_modules/get-uri'/name).read_bytes()==data
    private_members[name]=sha(data)
  get_uri_rows=[(p,row) for p,row in lock['packages'].items() if p and (row.get('name') or p.split('node_modules/')[-1])=='get-uri']
  assert len(get_uri_rows)==1 and get_uri_rows[0][0]=='node_modules/get-uri'
  assert get_uri_rows[0][1]['resolved']=='file:third_party/get-uri8-basicftp6-private.tgz' and get_uri_rows[0][1]['version']=='8.0.1'
  assert json.loads((runner/'node_modules/basic-ftp/package.json').read_text())['version']=='6.2.3'
  (out/'actual-private-get-uri-installed.json').write_text(json.dumps({'private_tar_sha256':sha(private_path.read_bytes()),'private_source_payloads':private_members,'all_actual_installed_payloads_match':True,'single_normal_GetURI_lock_row':get_uri_rows,'actual_BasicFTP_version':'6.2.3','original_Linux_SDK_cases_passed':258,'full_publisher_SDK_qualified':False},indent=2)+'\n')
 with (out/'full-audit.json').open('wb') as stream:
  result=subprocess.run(['npm','audit','--json'],cwd=runner,stdout=stream,stderr=subprocess.PIPE,check=False)
 (out/'full-audit.stderr.log').write_bytes(result.stderr)
 commands.append({'label':'full-unfiltered-audit','command':['npm','audit','--json'],'cwd':'services/promptfoo-runner','returncode':result.returncode})
 audit=json.loads((out/'full-audit.json').read_text());assert isinstance(audit['metadata']['vulnerabilities']['total'],int)
 rows=lock['packages'];version=lambda v:tuple(map(int,v.split('.'))) if re.fullmatch(r'\d+\.\d+\.\d+',v or '') else None
 lowered=[];removed=[];preview=[];names=set()
 for path,row in rows.items():
  if not path:continue
  name=row.get('name') or path.split('node_modules/')[-1];names.add(name)
  if row.get('version') and version(row['version']) is None:preview.append({'path':path,'version':row['version']})
 for path,old in baseline['packages'].items():
  if not path:continue
  if path not in rows:removed.append({'path':path,'version':old.get('version')});continue
  previous=version(old.get('version'));current=version(rows[path].get('version'))
  if previous and current and current<previous:lowered.append({'path':path,'before':old['version'],'after':rows[path]['version']})
 with ThreadPoolExecutor(max_workers=8) as pool:catalog=list(pool.map(public,sorted(names)))
 selected={row['name']:row for row in catalog};gaps=[]
 for path,row in rows.items():
  if not path:continue
  name=row.get('name') or path.split('node_modules/')[-1];target=selected[name]
  if target.get('error') or row.get('version')!=target['highest_stable']:gaps.append({'path':path,'name':name,'selected':row.get('version'),'highest_stable':target.get('highest_stable'),'error':target.get('error')})
 (out/'complete-public-namespace-catalog.json').write_text(json.dumps(catalog,indent=2)+'\n')
 (out/'complete-graph-delta.json').write_text(json.dumps({'lowered_components':lowered,'removed_package_paths':removed,'prerelease_or_nonstable_rows':preview,'highest_stable_gaps':gaps},indent=2)+'\n')
 original_case_exit=run('original-runner-cases',['node','--test','test_app.mjs']) if install_exit==0 else None
 # Preserve the original CI build command, Dockerfile commands and synthetic loopback case exactly.
 docker_exit=run('original-runner-image',['docker','build','--file','Dockerfile','--tag','hai-promptfoo-runner:ci','.']) if install_exit==0 else None
 if docker_exit==0:
  with (out/'image-inspect.json').open('wb') as stream:subprocess.run(['docker','image','inspect','hai-promptfoo-runner:ci'],stdout=stream,check=True)
 summary.update(direct_targets=expected['dependencies'],all_lock_namespace_count=len(names),registry_query_errors=sum(bool(row.get('error')) for row in catalog),complete_install_exit=install_exit,unfiltered_audit_totals=audit['metadata']['vulnerabilities'],audit_exit=result.returncode,lowered_components=lowered,removed_package_path_count=len(removed),nonstable_rows=preview,highest_stable_gap_count=len(gaps),original_runner_case_exit=original_case_exit,original_Docker_build_and_synthetic_CLI_case_exit=docker_exit,complete_bundled_component_and_original_publisher_SDK_inventory_not_qualified=True)
 summary['normal_graph_qualified']=install_exit==0 and result.returncode==0 and audit['metadata']['vulnerabilities']['total']==0 and not lowered and not preview and not gaps and original_case_exit==0 and docker_exit==0
except Exception as error:
 summary['failure']=type(error).__name__+': '+str(error)[:300]
 summary['normal_graph_qualified']=False
finally:
 preserved={}
 for name,row in source.items():
  if name in allowed:continue
  assert sha((root/name).read_bytes())==row['sha256'],name
  preserved[name]=row
 (out/'source-after.json').write_text(json.dumps({'head':head,'tree':tree,'all_other_payloads_unchanged':preserved,'only_generated_paths_allowed':sorted(allowed)},indent=2)+'\n')
 (out/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
 (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary))
sys.exit(0 if summary.get('normal_graph_qualified') else 1)
