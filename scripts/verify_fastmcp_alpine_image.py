"""Separate explicit candidate policy; retain the existing Debian verifier."""
from pathlib import Path
import json
import re
import sys


def main():
    scan_path, identity_path, service = map(Path, sys.argv[1:])
    assert service.name == 'fastmcp-bridge' and (service / 'Dockerfile.alpine').is_file()
    assert json.loads((service / 'native-runtime.profile.json').read_text()) == {'python': '3.15.1'}
    scan, identity = json.loads(scan_path.read_text()), json.loads(identity_path.read_text())
    assert identity['verified_descriptor_chain']
    assert scan['SchemaVersion'] == 2 and scan['ArtifactType'] == 'container_image'
    assert scan['Metadata']['ImageID'] == identity['config_id']
    operating_system = scan['Metadata']['OS']
    assert operating_system['Family'] == 'alpine' and str(operating_system['Name']).startswith('3.24')
    assert not operating_system.get('EOSL', False)
    installed, findings = {}, []
    os_count = python_count = 0
    assert scan.get('Results')
    for row in scan['Results']:
        assert row.get('Packages'), 'Complete package inventory is required'
        if row.get('Class') == 'os-pkgs':
            os_count += len(row['Packages'])
        if row.get('Type') == 'python-pkg':
            python_count += len(row['Packages'])
        for key in ['Vulnerabilities', 'ExperimentalModifiedFindings']:
            findings.extend(row.get(key) or [])
        for package in row['Packages']:
            version = str(package['Version'])
            if package.get('Release'):
                version += '-' + str(package['Release'])
            if package.get('Epoch') not in {None, 0, '0'}:
                version = str(package['Epoch']) + ':' + version
            name = re.sub('[-_.]+', '-', package['Name'].lower())
            installed.setdefault(name, set()).add(version)
    assert os_count and python_count, 'Both full OS and Python inventory are required'
    assert not installed.get('pip'), 'Package installer remains in the runtime'
    selected = dict(re.findall(r'(?m)^([\w.-]+)==([^\s;\\]+)', (service / 'requirements.lock').read_text()))
    assert selected
    for name, version in selected.items():
        assert re.fullmatch(r'\d+(?:\.\d+)*(?:\.post\d+)?', version), 'Nonstable locked version'
        assert version in installed.get(re.sub('[-_.]+', '-', name.lower()), set()), name
    assert installed.get('hai-openssl4') == {'4.1.0-r0'}, 'Native OpenSSL package inventory missing'
    assert installed.get('hai-sqlite3') == {'3.54.1-r0'}, 'Native SQLite package inventory missing'
    print(json.dumps({'passed': not findings, 'config_id': identity['config_id'],
        'os_packages': os_count, 'python_packages': python_count,
        'locked_versions_verified': len(selected), 'raw_or_modified_findings': len(findings),
        'advisory_exclusions': False, 'all_components_current': False,
        'full_vendor_acceptance': False, 'deployment_acceptance': False}))
    assert not findings, 'All raw or modified findings remain mandatory failures'


if __name__ == '__main__':
    main()
