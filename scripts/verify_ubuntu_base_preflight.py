"""Require complete supported Ubuntu inventory and every raw finding to pass."""
from pathlib import Path
import json
import sys


def main():
    scan_path, identity_path, inventory_path, log_path = map(Path, sys.argv[1:])
    scan = json.loads(scan_path.read_text())
    identity = json.loads(identity_path.read_text())
    inventory = json.loads(inventory_path.read_text())
    assert identity['verified_descriptor_chain']
    assert scan['SchemaVersion'] == 2 and scan['ArtifactType'] == 'container_image'
    assert scan['Metadata']['ImageID'] == identity['config_id']
    os_info = scan['Metadata']['OS']
    assert os_info['Family'] == 'ubuntu' and str(os_info['Name']) == '26.10'
    assert not os_info.get('EOSL', False)
    assert inventory['os_release']['ID'] == 'ubuntu'
    assert inventory['os_release']['VERSION_ID'] == '26.10'
    assert inventory['lsb_release']['DISTRIB_ID']=='Ubuntu'
    assert inventory['lsb_release']['DISTRIB_RELEASE']==inventory['os_release']['VERSION_ID']
    assert inventory['lsb_release']['DISTRIB_CODENAME']==inventory['os_release']['UBUNTU_CODENAME']
    log = log_path.read_text().lower()
    assert 'unsupported' not in log and 'not supported' not in log
    findings = []
    installed = {}
    os_count = 0
    go_stdlib_versions=set()
    assert scan.get('Results')
    for row in scan['Results']:
        assert row.get('Packages'), 'Complete package inventory is required'
        for field in ['Vulnerabilities', 'ExperimentalModifiedFindings']:
            findings.extend(row.get(field) or [])
        if row.get('Type')=='gobinary':
            assert row['Target']=='usr/bin/pebble'
            go_stdlib_versions.update(p['Version'] for p in row['Packages'] if p['Name']=='stdlib')
        if row.get('Class') == 'os-pkgs':
            os_count += len(row['Packages'])
            for package in row['Packages']:
                version = str(package['Version'])
                if package.get('Release'):
                    version += '-' + str(package['Release'])
                if package.get('Epoch') not in {None, 0, '0'}:
                    version = str(package['Epoch']) + ':' + version
                installed.setdefault(package['Name'].split(':')[0], set()).add(version)
    assert go_stdlib_versions=={'v1.27.3'}
    assert os_count and inventory['installed_packages']
    for name, version in inventory['installed_packages'].items():
        assert version in installed.get(name.split(':')[0], set()), name
    print(json.dumps({'passed': not findings, 'config_id': identity['config_id'],
        'os_packages': os_count, 'dpkg_versions_reconciled': len(inventory['installed_packages']),
        'raw_or_modified_findings': len(findings), 'advisory_exclusions': False,
        'native_or_Python_application_or_vendor_acceptance': False,
        'all_components_current': False, 'deployment_acceptance': False}))
    assert not findings, 'Every raw or modified finding remains fatal'


if __name__ == '__main__':
    main()
