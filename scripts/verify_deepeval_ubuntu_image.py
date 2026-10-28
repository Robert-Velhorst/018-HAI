"""Explicit full Ubuntu/DeepEval scan policy; existing distribution guards stay intact."""
from pathlib import Path
import json
import re
import sys


def main():
    scan_path,identity_path,inventory_path,service=map(Path,sys.argv[1:])
    scan=json.loads(scan_path.read_text())
    identity=json.loads(identity_path.read_text())
    inventory=json.loads(inventory_path.read_text())
    assert service.name=='deepeval-runner' and (service/'Dockerfile.ubuntu').is_file()
    assert identity['verified_descriptor_chain']
    assert scan['SchemaVersion']==2 and scan['ArtifactType']=='container_image'
    assert scan['Metadata']['ImageID']==identity['config_id']
    operating_system=scan['Metadata']['OS']
    assert operating_system['Family']=='ubuntu' and operating_system['Name']=='26.10'
    assert not operating_system.get('EOSL',False)
    norm=lambda name:re.sub('[-_.]+','-',name.lower())
    installed={}
    os_versions={}
    counts={}
    findings=[]
    for row in scan['Results']:
        assert row.get('Packages'),'Every result requires a complete package inventory'
        counts[row['Type']]=counts.get(row['Type'],0)+len(row['Packages'])
        for field in ['Vulnerabilities','ExperimentalModifiedFindings']:
            findings.extend(row.get(field) or [])
        for package in row['Packages']:
            version=str(package['Version'])
            if package.get('Release'):
                version+='-'+str(package['Release'])
            if package.get('Epoch') not in {None,0,'0'}:
                version=str(package['Epoch'])+':'+version
            installed.setdefault(norm(package['Name']),set()).add(version)
            if row.get('Class')=='os-pkgs':
                os_versions[package['Name'].split(':')[0]]=version
    assert counts.get('ubuntu') and counts.get('python-pkg') and counts.get('gobinary')
    assert not installed.get('pip')
    assert os_versions=={k.split(':')[0]:v for k,v in inventory['installed_OS_packages'].items()}
    selected=dict(re.findall(r'(?m)^([\w.-]+)==([^\s;\\]+)',(service/'requirements.lock').read_text()))
    assert selected
    reviewed_local={}
    reviewed_prereleases={"opentelemetry-semantic-conventions":"0.66b1"}
    for name,version in selected.items():
        if '+' in version:
            assert reviewed_local.get(norm(name))==version
        elif reviewed_prereleases.get(norm(name))==version:
            pass
        else:
            assert re.fullmatch(r'\d+(?:\.\d+)*(?:\.post\d+)?',version),'Prerelease dependency is not permitted'
        assert version in installed.get(norm(name),set()),name
    for distribution in inventory['python_distributions']:
        assert distribution['version'] in installed.get(norm(distribution['name']),set()),distribution['name']
    assert installed.get('hai-openssl4')=={'4.1.0-1hai1'}
    assert installed.get('hai-sqlite3')=={'3.54.1-1hai1'}
    print(json.dumps({'passed':not findings,'config_id':identity['config_id'],
        'complete_package_records':counts,'locked_versions_verified':len(selected),
        'raw_or_modified_findings':len(findings),'advisory_exclusions':False,
        'explicit_prerelease_exceptions':{n:v for n,v in selected.items() if reviewed_prereleases.get(norm(n))==v},
        'all_components_current':False,'full_vendor_acceptance':False,'deployment_acceptance':False}))
    assert not findings,'Every raw or modified finding remains fatal'


if __name__=='__main__':
    main()
