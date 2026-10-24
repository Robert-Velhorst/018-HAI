"""Verify the scanned image identity and complete selected package inventory."""
from pathlib import Path
import json
import re
import sys


def require(value, message):
    if not value:
        raise ValueError(message)


def normalized(value):
    return re.sub(r'[-_.]+', '-', value.lower())


def main():
    scan_path, identity_path, service_path = map(Path, sys.argv[1:])
    scan = json.loads(scan_path.read_text(encoding='utf-8'))
    identity = json.loads(identity_path.read_text(encoding='utf-8'))
    require(identity['verified_descriptor_chain'], 'Image descriptor proof is missing')
    require(scan['SchemaVersion'] == 2 and scan['ArtifactType'] == 'container_image',
            'Unexpected scan schema or artifact type')
    require(scan['Metadata']['ImageID'] == identity['config_id'], 'Scan selects a different image')
    os_info = scan['Metadata']['OS']
    require(not os_info.get('EOSL', False), 'The selected distribution is end of support')
    profile = service_path/'native-runtime.profile.json'
    if profile.exists():
        require(os_info['Family'] == 'debian' and str(os_info['Name']).split('.')[0] == '13',
                'Native runtime must retain Debian 13')
    else:
        alpine_wrappers = {'trivy-runner', 'gosec-runner'}
        debian_wrappers = {'gitleaks-runner', 'grype-runner', 'syft-runner',
                           'wasi-runner', 'whispercpp-runner', 'agent-framework-runner',
                           'crewai-runner', 'pydantic-ai-runner', 'deepteam-runner'}
        require(service_path.name in alpine_wrappers | debian_wrappers,
                'Unexpected non-native scan target')
        if service_path.name in alpine_wrappers:
            require(os_info['Family'] == 'alpine' and str(os_info['Name']).startswith('3.24'),
                    'Scanner runtime must retain Alpine 3.24')
        else:
            require(os_info['Family'] == 'debian' and str(os_info['Name']).split('.')[0] == '13',
                    'Python/scanner runtime must retain Debian 13')
    require(scan.get('Results'), 'The scanner produced no inventory')
    installed = {}
    counts = {'os': 0, 'python': 0, 'gobinary': 0}
    findings = []
    for result in scan['Results']:
        require(result.get('Packages'), 'A scan result has no package inventory')
        for field in ['Vulnerabilities', 'ExperimentalModifiedFindings']:
            findings.extend(result.get(field) or [])
        if result.get('Class') == 'os-pkgs':
            counts['os'] += len(result['Packages'])
        if result.get('Type') == 'python-pkg':
            counts['python'] += len(result['Packages'])
        if result.get('Type') == 'gobinary':
            counts['gobinary'] += len(result['Packages'])
        for package in result['Packages']:
            version = str(package['Version'])
            if package.get('Release'):
                version += '-' + str(package['Release'])
            if package.get('Epoch') not in {None, 0, '0'}:
                version = str(package['Epoch']) + ':' + version
            installed.setdefault(normalized(package['Name']), set()).add(version)
    require(counts['os'], 'OS package coverage is incomplete')
    if os_info['Family'] == 'debian':
        require(installed.get('libacl1') == {'2.4.0-1'},
                'Source-qualified ACL package is missing from the actual image inventory')
    require(not installed.get('pip'), 'Package installer remains in the runtime image')
    stdlib_wrappers = {'trivy-runner', 'gosec-runner', 'gitleaks-runner', 'grype-runner',
                       'syft-runner', 'wasi-runner', 'whispercpp-runner'}
    go_wrappers = {'trivy-runner', 'gosec-runner', 'gitleaks-runner', 'grype-runner', 'syft-runner'}
    if service_path.name in stdlib_wrappers:
        require(not counts['python'], 'Unexpected third-party Python package remains in the standard-library runtime')
    else:
        require(counts['python'], 'Application Python package coverage is incomplete')
    if service_path.name in go_wrappers:
        require(counts['gobinary'], 'Scanner binary module inventory is missing')
    selected = {}
    for filename in ['requirements.lock', 'requirements-engine.lock']:
        path = service_path/filename
        if path.exists():
            selected.update(re.findall(r'(?m)^([\w.-]+)==([^\s;\\]+)', path.read_text(encoding='utf-8')))
    for name, version in selected.items():
        require(version in installed.get(normalized(name), set()),
                'Installed locked version is missing: '+name+'=='+version)
    if profile.exists():
        require(installed.get('hai-openssl4') == {'4.1.0-1hai1'}, 'Native OpenSSL package is missing')
        require(installed.get('hai-sqlite3') == {'3.54.1-1hai1'}, 'Native SQLite package is missing')
    summary = {'passed': not findings, 'config_id': identity['config_id'],
               'package_counts': counts, 'locked_versions_verified': len(selected),
               'raw_or_modified_findings': len(findings), 'advisory_exclusions': False,
               'all_components_current': False, 'deployment_acceptance': False}
    print(json.dumps(summary))
    require(not findings, 'Unresolved raw or modified image findings remain')


if __name__ == '__main__':
    main()
