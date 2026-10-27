"""Owned hosted metadata preparation; do not execute publisher tests behind resolver exceptions."""
from pathlib import Path
from urllib.request import urlopen
from urllib.parse import quote
import hashlib,json,os,re,subprocess,sys,shlex,shutil
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
assert os.environ.get('GITHUB_REPOSITORY')=='Robert-Velhorst/018-HAI'
root=Path.cwd();sdk=root/'.sdk/azure-identity-source';out=Path(os.environ['RUNNER_TEMP'])/'azure-identity-strict-source';out.mkdir()
commit='ef2a29238eed3348a7d0688f430d405abdbcaab9';tree='2bac2f189027f2d289bfad64294ad78584659054'
sha=lambda b:hashlib.sha256(b).hexdigest()
def git(folder,*args):return subprocess.check_output(['git','-C',str(folder),*args])
def snapshot(folder):
 payloads={};gitlinks={}
 for entry in git(folder,'ls-tree','-r','-z','HEAD').split(b'\0'):
  if not entry:continue
  meta,name=entry.split(b'\t',1);mode,kind,oid=meta.decode().split();name=name.decode()
  if kind=='commit':assert mode=='160000';gitlinks[name]={'mode':mode,'commit':oid};continue
  assert kind=='blob' and mode in {'100644','100755','120000'}
  data=os.readlink(folder/name).encode() if mode=='120000' else (folder/name).read_bytes()
  assert hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()==oid,name
  payloads[name]={'blob':oid,'sha256':sha(data),'mode':mode}
 return {'head':git(folder,'rev-parse','HEAD').decode().strip(),'tree':git(folder,'rev-parse','HEAD^{tree}').decode().strip(),'payloads':payloads,'unexpanded_gitlinks':gitlinks}
project=snapshot(root);(out/'project-source-before.json').write_text(json.dumps(project,indent=2)+'\n')
original=snapshot(sdk);assert original['head']==commit and original['tree']==tree
(out/'publisher-source-before.json').write_text(json.dumps(original,indent=2)+'\n')
allowed={'package.json','pnpm-workspace.yaml','.npmrc','sdk/identity/identity/package.json','pnpm-lock.yaml','common/tools/eslint-plugin-azure-sdk/package.json'}
commands=[];summary={'head':project['head'],'publisher_commit':commit,'publisher_tree':tree,'original_SDK_case_build_install_matrix_or_compiler_acceptance':False,'application_adoption':False,'original_cases_helpers_fixtures_assertions_selectors_timeouts_or_compiler_files_changed':False}
try:
 with urlopen('https://api.github.com/repos/Azure/azure-sdk-for-js/git/commits/'+commit,timeout=40) as response:metadata=json.load(response)
 assert metadata['sha']==commit and metadata['tree']['sha']==tree and metadata['verification']['verified']
 (out/'publisher-verified-commit.json').write_text(json.dumps(metadata,indent=2)+'\n')
 for name in sorted(allowed):
  (out/('before-'+name.replace('/','__'))).write_bytes((sdk/name).read_bytes())
 assert not any((sdk/name).exists() for name in ['.pnpmfile.cjs','.pnpmfile.mjs','.pnpmfile.js'])
 root_manifest=json.loads((sdk/'package.json').read_text());root_manifest['packageManager']='pnpm@12.16.1'
 (sdk/'package.json').write_text(json.dumps(root_manifest,indent=2)+'\n')
 manifest_path=sdk/'sdk/identity/identity/package.json';identity=json.loads(manifest_path.read_text());assert identity['name']=='@azure/identity' and identity['version']=='4.14.0' and identity['dependencies']['@azure/msal-node']=='^6.0.0'
 identity['dependencies']['@azure/msal-node']='^7.1.1';manifest_path.write_text(json.dumps(identity,indent=2)+'\n')
 # Capture the highest public nondeprecated stable release, not an old lock or tag alone.
 names=['typescript','typescript-eslint','eslint','@eslint/js','@typescript-eslint/eslint-plugin','@typescript-eslint/parser','@typescript-eslint/rule-tester','@typescript-eslint/utils','@typescript-eslint/typescript-estree']
 releases={}
 for name in names:
  with urlopen('https://registry.npmjs.org/'+quote(name,safe=''),timeout=40) as response:packument=json.load(response)
  stable=[version for version,row in packument['versions'].items() if re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)',version) and not row.get('deprecated')]
  version=max(stable,key=lambda value:tuple(map(int,value.split('.'))))
  with urlopen('https://registry.npmjs.org/'+quote(name,safe='')+'/'+version,timeout=40) as response:release=json.load(response)
  assert release['name']==name and release['version']==version and not release.get('deprecated')
  releases[name]=release
 (out/'current-public-tooling-releases.json').write_text(json.dumps(releases,indent=2)+'\n')
 plugin_path=sdk/'common/tools/eslint-plugin-azure-sdk/package.json';plugin=json.loads(plugin_path.read_text())
 changed_declarations={}
 for section in ['dependencies','devDependencies','peerDependencies']:
  for name,version_range in list(plugin.get(section,{}).items()):
   if name in releases and not version_range.startswith(('workspace:','catalog:')):
    new_range='^'+releases[name]['version']
    plugin[section][name]=new_range;changed_declarations[section+'/'+name]={'before':version_range,'after':new_range}
 plugin_path.write_text(json.dumps(plugin,indent=2)+'\n')
 summary['current_public_tooling_versions']={name:row['version'] for name,row in releases.items()}
 summary['tooling_parent_declaration_changes']=changed_declarations
 workspace=(sdk/'pnpm-workspace.yaml').read_text();lines=workspace.splitlines(keepends=True);result=[];removed={};i=0
 forbidden={'overrides','peerDependencyRules','minimumReleaseAgeExclude'}
 while i<len(lines):
  match=re.match(r'^([A-Za-z][A-Za-z0-9]*):',lines[i])
  if match and match[1] in forbidden:
   key=match[1];block=[lines[i]];i+=1
   while i<len(lines) and (not lines[i].strip() or lines[i][0].isspace()):block.append(lines[i]);i+=1
   removed[key]=''.join(block);continue
  result.append(lines[i]);i+=1
 assert set(removed)==forbidden
 strict_workspace=''.join(result)+'\nstrictPeerDependencies: true\nengineStrict: true\n';assert not any(re.search(r'^'+re.escape(key)+':',strict_workspace,re.M) for key in forbidden)
 for name in ['typescript','typescript-eslint','eslint','@eslint/js']:
  pattern=r'(?m)^(  '+re.escape("'"+name+"'" if name.startswith('@') else name)+r': )[^\r\n]+$'
  strict_workspace,count=re.subn(pattern,lambda match:match[1]+'^'+releases[name]['version'],strict_workspace)
  assert count==1,name
 (sdk/'pnpm-workspace.yaml').write_text(strict_workspace)
 old_npmrc=(sdk/'.npmrc').read_text();assert [line for line in old_npmrc.splitlines() if line.strip()]==['registry=https://pkgs.dev.azure.com/azure-sdk/public/_packaging/azure-sdk-for-js/npm/registry/']
 (sdk/'.npmrc').write_text('registry=https://registry.npmjs.org/\n')
 compiler_source=sdk/'common/tools/eslint-plugin-azure-sdk/src/configs/index.ts'
 summary['publisher_compiler_assertion_shim_present_but_not_executed_or_qualified']='bypassTypeCheck' in compiler_source.read_text()
 pnpm_path=shutil.which('pnpm');assert pnpm_path
 resolved_pnpm=Path(pnpm_path).resolve();launcher_bytes=resolved_pnpm.read_bytes()
 (out/'pnpm-launcher.json').write_text(json.dumps({'PATH_entry':pnpm_path,'resolved':str(resolved_pnpm),'sha256':sha(launcher_bytes),'header_hex':launcher_bytes[:16].hex(),'declares_shebang':launcher_bytes.startswith(b'#!'),'normal_CI_shell_invocation':['bash','-c'],'platform_or_engine_checks_disabled':False},indent=2)+'\n')
 with (out/'pnpm-version.log').open('wb') as stream:version_check=subprocess.run(['bash','-c','pnpm --version'],cwd=sdk,stdout=stream,stderr=subprocess.STDOUT,check=False)
 commands.append({'label':'normal-shell-package-manager-version','command':['pnpm','--version'],'launcher':['bash','-c'],'cwd':'.sdk/azure-identity-source','returncode':version_check.returncode,'not_an_original_install_build_or_case_result':True})
 assert version_check.returncode==0 and (out/'pnpm-version.log').read_text().strip().endswith('12.16.1')
 effective={}
 for setting in ['strictPeerDependencies','engineStrict']:
  config_command=['pnpm','config','get',setting]
  checked=subprocess.run(['bash','-c',shlex.join(config_command)],cwd=sdk,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=False)
  (out/('effective-'+setting+'.log')).write_bytes(checked.stdout)
  commands.append({'label':'verify-effective-'+setting,'command':config_command,'launcher':['bash','-c'],'returncode':checked.returncode,'not_an_original_install_build_or_case_result':True})
  assert checked.returncode==0 and checked.stdout.decode().strip()=='true',setting
  effective[setting]=True
 summary['verified_effective_pnpm_settings']=effective
 command=['pnpm','update','--recursive','--lockfile-only']
 with (out/'strict-normal-lock-preflight.log').open('wb') as stream:resolution=subprocess.run(['bash','-c',shlex.join(command)],cwd=sdk,stdout=stream,stderr=subprocess.STDOUT,check=False)
 commands.append({'label':'additional-strict-normal-lock-preparation','command':command,'launcher':['bash','-c'],'cwd':'.sdk/azure-identity-source','returncode':resolution.returncode,'not_an_original_install_build_or_case_result':True})
 summary.update(normal_lock_preflight_exit=resolution.returncode,removed_resolver_exception_keys=sorted(removed),public_registry='https://registry.npmjs.org/',strict_peer_dependencies=True,engine_strict=True,proposed_MSAL_range='^7.1.1',proposed_SDK_package_manager='pnpm@12.16.1',metadata_preparation_only=True)
 if resolution.returncode==0:
  disk=shutil.disk_usage(sdk);summary['standard_worker_free_disk_before_original_install']=disk.free
  if disk.free<8*1024**3:
   summary['original_SDK_install_not_attempted']='Less than8GiB free on the standard owned worker; no larger paid runner selected.'
   summary['original_SDK_install_exit']=None
  else:
   summary['metadata_preparation_only']=False
   original_install=['pnpm','install']
   with (out/'original-SDK-install-on-strict-source.log').open('wb') as stream:installed=subprocess.run(['bash','-c',shlex.join(original_install)],cwd=sdk,stdout=stream,stderr=subprocess.STDOUT,check=False)
   commands.append({'label':'original-SDK-install-on-strict-source','command':original_install,'launcher':['bash','-c'],'cwd':'.sdk/azure-identity-source','returncode':installed.returncode,'not_a_build_or_case_result':True})
   summary['original_SDK_install_exit']=installed.returncode
except Exception as error:
 summary.update(failure=type(error).__name__+': '+str(error)[:400],normal_lock_preflight_exit=None)
finally:
 preserved={}
 for name,row in original['payloads'].items():
  if name in allowed:continue
  current=os.readlink(sdk/name).encode() if row['mode']=='120000' else (sdk/name).read_bytes()
  assert sha(current)==row['sha256'],name;preserved[name]=row
 (out/'publisher-source-after.json').write_text(json.dumps({'head':commit,'tree':tree,'all_other_payloads_unchanged':preserved,'only_preparation_paths_allowed':sorted(allowed),'unexpanded_gitlinks':original['unexpanded_gitlinks']},indent=2)+'\n')
 for name in sorted(allowed):
  if (sdk/name).exists():(out/('prepared-'+name.replace('/','__'))).write_bytes((sdk/name).read_bytes())
 for name,row in project['payloads'].items():
  data=os.readlink(root/name).encode() if row['mode']=='120000' else (root/name).read_bytes()
  assert sha(data)==row['sha256'],name
 (out/'project-source-after.json').write_text(json.dumps(project,indent=2)+'\n')
 (out/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
 (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary))
# Successful preparation is still not SDK acceptance; existing typing/source gates remain explicit.
sys.exit(0 if summary.get('normal_lock_preflight_exit')==0 and summary.get('original_SDK_install_exit')==0 else 1)
