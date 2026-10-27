"""Run unchanged publisher SDK commands only on the owned hosted worker."""
from pathlib import Path,PurePosixPath
from urllib.request import urlopen
import base64,hashlib,io,json,os,subprocess,sys,tarfile
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
root=Path.cwd();out=Path(os.environ['RUNNER_TEMP'])/'get-uri-basic-ftp-original-sdk';out.mkdir()
publisher='TooTallNate/proxy-agents';sdk_commit='b7e5f7ccce1a3ac5b339cc4c587974e8989cbc16';sdk_tree='a39ee2fc198f46cda3d1ea692086a513b851e459'
sha=lambda b:hashlib.sha256(b).hexdigest()
def git(folder,*args):return subprocess.check_output(['git','-C',str(folder),*args])
def source(folder):
 data={}
 for row in git(folder,'ls-tree','-r','-z','HEAD').split(b'\0'):
  if not row:continue
  meta,name=row.split(b'\t',1);mode,kind,blob=meta.decode().split();name=name.decode();assert kind=='blob' and mode in {'100644','100755'}
  payload=(folder/name).read_bytes();assert hashlib.sha1(b'blob '+str(len(payload)).encode()+b'\0'+payload).hexdigest()==blob,name
  data[name]={'blob':blob,'sha256':sha(payload)}
 return {'head':git(folder,'rev-parse','HEAD').decode().strip(),'tree':git(folder,'rev-parse','HEAD^{tree}').decode().strip(),'payloads':data}
commands=[]
def run(label,command,cwd):
 with (out/(label+'.log')).open('wb') as stream:result=subprocess.run(command,cwd=cwd,stdout=stream,stderr=subprocess.STDOUT,check=False)
 commands.append({'label':label,'command':command,'cwd':str(cwd.relative_to(root)),'returncode':result.returncode})
 return result.returncode

def archive_members(raw):
 result={}
 with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as archive:
  for item in archive:
   assert item.isfile() and item.name.startswith('package/')
   name=item.name[len('package/'):];assert name and '..' not in PurePosixPath(name).parts and name not in result
   result[name]=archive.extractfile(item).read()
 return result

def actual_basic_ftp(folder,stage):
 location=(folder/'packages/get-uri/node_modules/basic-ftp').resolve();manifest=json.loads((location/'package.json').read_text());version=manifest['version']
 with urlopen('https://registry.npmjs.org/basic-ftp/'+version,timeout=40) as response:metadata=json.load(response)
 with urlopen(metadata['dist']['tarball'],timeout=40) as response:raw=response.read(5*1024*1024+1)
 assert len(raw)<=5*1024*1024 and metadata['dist']['integrity']=='sha512-'+base64.b64encode(hashlib.sha512(raw).digest()).decode()
 members=archive_members(raw)
 for name,data in members.items():assert (location/name).read_bytes()==data,name
 (out/(stage+'-actual-basic-ftp.json')).write_text(json.dumps({'version':version,'registry_tar_sha256':sha(raw),'registry_integrity':metadata['dist']['integrity'],'all_actual_installed_payloads_match':{name:sha(data) for name,data in members.items()}},indent=2)+'\n')
 return version

project=source(root);(out/'project-source-before.json').write_text(json.dumps(project,indent=2)+'\n')
summary={'head':project['head'],'publisher_commit':sdk_commit,'complete_publisher_SDK_qualified':False,'original_VPN_E2E_or_Vercel_pack_workflow_executed':False,'original_case_fixture_helper_assertion_selector_timeout_or_command_changes':False,'application_runtime_dependency_or_override_changes':False}
try:
 with urlopen('https://api.github.com/repos/'+publisher+'/git/commits/'+sdk_commit,timeout=40) as response:metadata=json.load(response)
 assert metadata['sha']==sdk_commit and metadata['tree']['sha']==sdk_tree and metadata['verification']['verified'] is True
 (out/'publisher-verified-commit.json').write_text(json.dumps(metadata,indent=2)+'\n')
 sdk_before={};stages={};original_commands=[['pnpm','test'],['pnpm','lint'],['pnpm','format:check'],['node','scripts/check-deps.mjs','engines'],['node','scripts/check-deps.mjs','licenses']]
 for stage,path in [('baseline','.sdk/get-uri-original'),('port','.sdk/get-uri-basicftp6')]:
  folder=root/path;before=source(folder);assert before['head']==sdk_commit and before['tree']==sdk_tree
  assert len(before['payloads'])==202
  sdk_before[stage]=before
  (out/(stage+'-source-before.json')).write_text(json.dumps(before,indent=2)+'\n')
 assert sdk_before['baseline']==sdk_before['port']
 with urlopen('https://registry.npmjs.org/get-uri/8.0.1',timeout=40) as response:published=json.load(response)
 with urlopen(published['dist']['tarball'],timeout=40) as response:published_raw=response.read(5*1024*1024+1)
 assert len(published_raw)<=5*1024*1024 and published['dist']['integrity']=='sha512-'+base64.b64encode(hashlib.sha512(published_raw).digest()).decode()
 (out/'public-get-uri8-package.json').write_text(json.dumps({'name':published['name'],'version':published['version'],'dist':published['dist'],'all_payloads':{p:sha(b) for p,b in archive_members(published_raw).items()}},indent=2)+'\n')
 for stage,path in [('baseline','.sdk/get-uri-original'),('port','.sdk/get-uri-basicftp6')]:
  folder=root/path;before=sdk_before[stage];results=[]
  manifest_path=folder/'packages/get-uri/package.json';original_manifest=manifest_path.read_bytes();(out/(stage+'-original-package.json')).write_bytes(original_manifest)
  if stage=='port':
   assert run('port-normal-dependency-update',['pnpm','--filter','get-uri','add','basic-ftp@^6.2.3'],folder)==0
   manifest=json.loads(manifest_path.read_text());expected=json.loads(original_manifest);expected['dependencies']['basic-ftp']='^6.2.3';assert manifest==expected
  install=run(stage+'-original-frozen-install',['pnpm','install','--frozen-lockfile'],folder)
  if install==0:
   ftp_version=actual_basic_ftp(folder,stage)
   if stage=='port':assert ftp_version=='6.2.3'
   else:assert ftp_version=='5.3.1'
   for i,command in enumerate(original_commands):results.append({'command':command,'exit':run(stage+'-original-command-'+str(i),command,folder)})
   package_dir=folder/'packages/get-uri'
   pack_exit=run(stage+'-additional-private-pack',['pnpm','pack'],package_dir)
   if pack_exit==0:
    tarballs=list(package_dir.glob('get-uri-8.0.1.tgz'));assert len(tarballs)==1
    raw=tarballs[0].read_bytes();members=archive_members(raw)
    assert json.loads(members['package.json'])['dependencies']['basic-ftp']==('^6.2.3' if stage=='port' else '^5.3.1')
    (out/(stage+'-private-package.tgz')).write_bytes(raw)
    (out/(stage+'-private-package-payloads.json')).write_text(json.dumps({'sha256':sha(raw),'members':{p:sha(b) for p,b in members.items()}},indent=2)+'\n')
  allowed={'packages/get-uri/package.json','pnpm-lock.yaml'} if stage=='port' else set()
  preserved={}
  for name,row in before['payloads'].items():
   if name in allowed:continue
   assert sha((folder/name).read_bytes())==row['sha256'],stage+':'+name
   preserved[name]=row
  (out/(stage+'-source-after.json')).write_text(json.dumps({'head':sdk_commit,'tree':sdk_tree,'all_other_payloads_unchanged':preserved,'only_generated_paths_allowed':sorted(allowed)},indent=2)+'\n')
  (out/(stage+'-actual-package.json')).write_bytes(manifest_path.read_bytes())
  (out/(stage+'-actual-pnpm-lock.yaml')).write_bytes((folder/'pnpm-lock.yaml').read_bytes())
  stages[stage]={'frozen_install_exit':install,'original_commands':results,'additional_private_pack_exit':pack_exit if install==0 else None,'original_publisher_source_payloads':len(before['payloads']),'unchanged_original_payloads':len(preserved)}
 summary['stages']=stages
 if (out/'baseline-private-package.tgz').exists() and (out/'port-private-package.tgz').exists():
  baseline=archive_members((out/'baseline-private-package.tgz').read_bytes());port=archive_members((out/'port-private-package.tgz').read_bytes());assert set(baseline)==set(port)
  changed=[p for p in baseline if baseline[p]!=port[p]]
  summary['private_archive_changed_member_paths']=changed
  # Any extra compiled-source difference remains visible and must be reviewed.
  (out/'private-package-member-comparison.json').write_text(json.dumps({'all_paths':sorted(port),'changed_payloads':changed,'public_upstream_release':False,'private_compatibility_port':True},indent=2)+'\n')
 summary['Linux_original_unit_build_lint_format_engine_and_license_commands_pass']=all(s['frozen_install_exit']==0 and len(s['original_commands'])==len(original_commands) and all(c['exit']==0 for c in s['original_commands']) and s['additional_private_pack_exit']==0 for s in stages.values())
except Exception as error:
 summary['failure']=type(error).__name__+': '+str(error)[:400]
 summary['Linux_original_unit_build_lint_format_engine_and_license_commands_pass']=False
finally:
 for name,row in project['payloads'].items():assert sha((root/name).read_bytes())==row['sha256'],name
 (out/'project-source-after.json').write_text(json.dumps(project,indent=2)+'\n')
 (out/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
 (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary))
sys.exit(0 if summary.get('Linux_original_unit_build_lint_format_engine_and_license_commands_pass') and summary.get('private_archive_changed_member_paths')==['package.json'] else 1)
