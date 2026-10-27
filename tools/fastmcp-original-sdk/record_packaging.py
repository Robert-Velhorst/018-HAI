"""Retain exact approved command differences and packaging inventories on hosted CI."""
from pathlib import Path
import hashlib,json,os,subprocess,sys

assert os.environ.get('GITHUB_ACTIONS')=='true'
assert os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
reports=Path(os.environ['RUNNER_TEMP'])/'hai-fastmcp-original-sdk'
spec=json.loads((reports/'spec.json').read_text())
raw=Path('.github/workflows/run-tests.yml').read_bytes()
assert hashlib.sha256(raw).hexdigest()==spec['approved_packaging_exception']['publisher_workflow_sha256']
text=raw.decode()
start=text.index('      - name: Build package wheels\n')
import re
following=re.search(r'(?m)^  [a-zA-Z_][\w-]*:\s*$',text[start:])
end=start+following.start() if following else len(text)
original=text[start:end].rstrip()+'\n'
executed=original.replace('--prerelease=allow','--prerelease=disallow')
assert original.count('--prerelease=allow')==2
assert hashlib.sha256(original.encode()).hexdigest()==spec['approved_packaging_exception']['original_steps_sha256']
assert hashlib.sha256(executed.encode()).hexdigest()==spec['approved_packaging_exception']['executed_steps_sha256']
(reports/'original-packaging-steps.yml').write_text(original)
(reports/'executed-packaging-steps.yml').write_text(executed)
record={'publisher_commit':spec['publisher_commit'],'exact_approved_changes':2,
        'original_steps_sha256':hashlib.sha256(original.encode()).hexdigest(),
        'executed_steps_sha256':hashlib.sha256(executed.encode()).hexdigest(),
        'assertions_build_commands_and_other_arguments_unchanged':True,
        'phase':sys.argv[1]}
if sys.argv[1]=='after':
    record['wheels']={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(Path('/tmp/fastmcp-dist').glob('*.whl'))}
    record['installed_distributions']={}
    query="import importlib.metadata as m,json;print(json.dumps(sorted([{'name':d.metadata['Name'],'version':d.version} for d in m.distributions()],key=lambda r:r['name'].lower())))"
    for kind in ['slim-bare','slim-client','slim-server','full','remote']:
        python=Path('/tmp')/('fastmcp-'+kind+'-smoke')/'bin/python'
        if python.exists():
            record['installed_distributions'][kind]=json.loads(subprocess.check_output([str(python),'-c',query]))
(reports/('packaging-'+sys.argv[1]+'.json')).write_text(json.dumps(record,indent=2)+'\n')
