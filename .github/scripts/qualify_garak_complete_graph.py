"""Port complete upstream dependency metadata; preserve all original SDK code."""
import argparse,datetime,hashlib,importlib.metadata,json,pathlib,re,subprocess,tomllib
parser=argparse.ArgumentParser()
parser.add_argument('mode',choices=['prepare','audit'])
args=parser.parse_args()
owner=pathlib.Path(__file__).resolve().parents[2]
vendor=owner/'vendor-garak'
evidence=owner/'garak-complete-graph-evidence'
evidence.mkdir(exist_ok=True)
inputs=json.loads((owner/'.github/qualifications/garak-complete-stable-inputs.json').read_bytes())
def tracked():
 names=subprocess.check_output(['git','-C',str(vendor),'ls-files','-z']).split(b'\0')
 return {n.decode():hashlib.sha256((vendor/n.decode()).read_bytes()).hexdigest() for n in names if n and (vendor/n.decode()).is_file()}
def pinned(row):
 original=row['requirement']
 marker=(';'+original.split(';',1)[1]) if ';' in original else ''
 return row['name']+'=='+row['highest_numeric_stable']+marker
if args.mode=='prepare':
 head=subprocess.check_output(['git','-C',str(vendor),'rev-parse','HEAD']).decode().strip()
 assert head==inputs['upstream_commit']
 before=tracked()
 assert before['pyproject.toml']==inputs['upstream_pyproject_sha256']
 original=(vendor/'pyproject.toml').read_text()
 doc=tomllib.loads(original)
 groups={'required':doc['project']['dependencies'],**doc['project']['optional-dependencies']}
 assert groups==inputs['groups']
 assert len(inputs['inputs'])==60
 replacement={r['requirement']:pinned(r) for r in inputs['inputs']}
 assert len(replacement)==60
 new=original
 for old,value in replacement.items():
  tokens=[quote+old+quote for quote in ['"',"'"]]
  token=next(t for t in tokens if t in new)
  assert new.count(token)==1
  new=new.replace(token,json.dumps(value,ensure_ascii=False),1)
 parsed=tomllib.loads(new)
 assert parsed['project']['version']==doc['project']['version']==inputs['upstream_version']
 expected=json.loads(json.dumps(doc))
 expected['project']['dependencies']=[replacement[r] for r in groups['required']]
 expected['project']['optional-dependencies']={k:[replacement[r] for r in v] for k,v in groups.items() if k!='required'}
 assert parsed==expected
 lines=(vendor/'requirements.txt').read_text().splitlines()
 found=[line for line in lines if line and not line.startswith('#')]
 assert len(found)==60 and set(found)==set(replacement)
 (vendor/'pyproject.toml').write_text(new,encoding='utf-8')
 (vendor/'requirements.txt').write_text('\n'.join(replacement.get(line,line) for line in lines)+'\n',encoding='utf-8')
 after=tracked()
 changed=[n for n in before if before[n]!=after[n]]
 assert set(changed)=={'pyproject.toml','requirements.txt'}
 record={'upstream_head':head,'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'all_tracked_original_files':before,'ported_files':{n:after[n] for n in changed},'all_original_SDK_cases_helpers_assertions_fixtures_selectors_timeouts_commands_preserved':True,'upstream_version_not_relabelled':True,'groups':{k:len(v) for k,v in groups.items()},'full_SDK_or_graph_qualified':False}
 (evidence/'source-preservation.json').write_text(json.dumps(record,indent=2)+'\n',encoding='utf-8')
 (evidence/'ported-pyproject.toml').write_text(new,encoding='utf-8')
 (evidence/'ported-requirements.txt').write_bytes((vendor/'requirements.txt').read_bytes())
 subprocess.run(['git','-C',str(vendor),'diff','--binary','--output='+str(evidence/'complete-stable-metadata-port.patch')],check=True)
 print(json.dumps({'original_payloads':len(before),'changed':changed,'inputs':60,'runtime_modified':False}))
else:
 record=json.loads((evidence/'source-preservation.json').read_bytes())
 after=tracked()
 before=record['all_tracked_original_files']
 assert after.keys()==before.keys()
 for name,digest in before.items():
  assert after[name]==record['ported_files'].get(name,digest),(name,'unexpected source mutation')
 installed=sorted([{'name':d.metadata['Name'],'version':d.version} for d in importlib.metadata.distributions()],key=lambda d:d['name'].lower())
 invalid=[d for d in installed if not re.fullmatch(r'[0-9]+(?:\.[0-9]+)*',d['version'])]
 result={'source_preserved':True,'installed':installed,'non_numeric_stable':invalid,'full_SDK_or_graph_qualified':False}
 (evidence/'installed-distribution-audit.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
 assert not invalid,invalid
 print(json.dumps({'source_preserved':True,'installed_distributions':len(installed),'non_numeric_stable':len(invalid)}))
