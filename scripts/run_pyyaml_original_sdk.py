"""Run original public PyYAML cases only on a disposable hosted SDK image."""
from pathlib import Path
import argparse
import hashlib
import importlib.metadata
import importlib.util
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET


def verify_source(root, expected):
    actual = {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in expected}
    if actual != expected:
        raise ValueError('Original publisher source/case bytes changed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--final-runtime', action='store_true')
    args = parser.parse_args()
    if (sys.platform != 'linux' or os.environ.get('HAI_DISPOSABLE_PUBLIC_SDK') != '1'
            or os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted'):
        raise RuntimeError('Vendor execution requires the disposable hosted SDK image')
    source = Path('/publisher-src')
    reports = Path('/reports')
    runtime_report = Path('/app/installation-report.json')
    test_report = Path('/app/pyyaml-test-sdk-installation-report.json')
    bundle_hashes = {}
    image_id = None
    if args.final_runtime:
        image_id = os.environ.get('HAI_RUNTIME_IMAGE_ID', '')
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', image_id) or os.geteuid() != 10001:
            raise ValueError('Expected the immutable final runtime image and non-root user')
        bundle = Path('/review/pytest-sdk')
        site = bundle / 'lib/python3.15/site-packages'
        expected = {'_pytest', 'pytest', 'py.py', 'pluggy', 'iniconfig', 'pytest-9.1.1.dist-info',
            'pluggy-1.7.0.dist-info', 'iniconfig-2.3.1.dist-info'}
        if {p.name for p in site.iterdir()} != expected:
            raise ValueError('Test bundle must not supply PyYAML or other runtime packages')
        if {p.name for p in (bundle / 'bin').iterdir()} != {'pytest', 'py.test'}:
            raise ValueError('Both original SDK CLI scripts must be retained')
        for path in sorted(bundle.rglob('*')):
            if path.is_symlink():raise ValueError('Unexpected test-bundle link')
            if path.is_file():bundle_hashes[str(path.relative_to(bundle))] = hashlib.sha256(path.read_bytes()).hexdigest()
        sys.path.insert(0, str(site))
        if not Path(importlib.util.find_spec('pytest').origin).is_relative_to(site):
            raise ValueError('The reviewed test-tool package must supply pytest')
        runtime_report = Path('/usr/share/doc/hai-native/installation-report.json')
        test_report = bundle / 'installation-report.json'
    identity = json.loads((source / 'hai-source-identity.json').read_text())
    expected = identity['payload_sha256']
    verify_source(source, expected)
    if len(expected) != 641 or not identity['all_publisher_source_verified']:
        raise ValueError('Expected complete publisher source')
    if importlib.metadata.version('pytest') != '9.1.1':
        raise ValueError('Current stable pytest SDK identity differs')
    installation = json.loads(runtime_report.read_text())
    installed = installation['install']
    if len(installed) != 83:
        raise ValueError('Expected all original runtime installation entries')
    for row in installed:
        package = row['metadata']
        if importlib.metadata.version(package['name']) != package['version']:
            raise ValueError('Test SDK installation changed the runtime package graph')
    import yaml
    import yaml._yaml
    extension = Path(yaml._yaml.__file__).resolve(strict=True)
    if not yaml.__with_libyaml__ or not extension.is_relative_to(Path('/opt/hai-venv')):
        raise ValueError('The real installed Mini-SWE PyYAML extension is required')
    linkage = subprocess.run(['ldd', str(extension)], text=True, capture_output=True, check=True)
    if 'not found' in linkage.stdout or 'libyaml' not in linkage.stdout:
        raise ValueError('The actual LibYAML provider must resolve')
    (reports / 'native-linkage.log').write_text(linkage.stdout + linkage.stderr)
    shutil.copyfile(runtime_report, reports / 'runtime-installation-report.json')
    shutil.copyfile(test_report, reports / 'test-sdk-installation-report.json')
    shutil.copyfile(source / 'hai-source-identity.json', reports / 'publisher-source-identity.json')
    environment = dict(os.environ, PYYAML_FORCE_LIBYAML='1', PYTHONDONTWRITEBYTECODE='1')
    if args.final_runtime:environment['PYTHONPATH'] = str(site)
    extension_before = hashlib.sha256(extension.read_bytes()).hexdigest()
    results = {}
    with tempfile.TemporaryDirectory(prefix='hai-original-pyyaml-') as name:
        checkout = Path(name) / 'publisher'
        shutil.copytree(source, checkout)
        for label, command in [
            ('all-original-cases', [sys.executable, '-m', 'pytest',
                '--junitxml=/reports/all-original-cases.xml']),
            ('original-publisher-smoke', [sys.executable, 'packaging/build/smoketest.py']),
        ]:
            with (reports / (label + '.log')).open('w') as log:
                result = subprocess.run(command, cwd=checkout, env=environment,
                    stdout=log, stderr=subprocess.STDOUT)
            results[label] = {'command': command, 'returncode': result.returncode}
            print((reports / (label + '.log')).read_text(), flush=True)
        verify_source(checkout, expected)
    verify_source(source, expected)
    cases = []
    xml = reports / 'all-original-cases.xml'
    if xml.exists():
        cases = list(ET.parse(xml).getroot().iter('testcase'))
    counts = {'reported': len(cases),
        'failed': sum(c.find('failure') is not None for c in cases),
        'errors': sum(c.find('error') is not None for c in cases),
        'skipped': sum(c.find('skipped') is not None for c in cases)}
    qualified = (all(r['returncode'] == 0 for r in results.values())
        and counts['reported'] > 0 and counts['failed'] == counts['errors'] == 0)
    if args.final_runtime:
        qualified = qualified and counts['reported'] == 2608 and counts['skipped'] == 0
    if hashlib.sha256(extension.read_bytes()).hexdigest() != extension_before:
        raise ValueError('Installed extension bytes changed during SDK cases')
    record = {'source_head': os.environ['GITHUB_SHA'],
        'publisher_commit': identity['publisher_commit'], 'source_payloads_verified': len(expected),
        'original_runtime_distribution_versions_verified': len(installed),
        'original_test_and_fixture_payloads_unchanged': sum(p.startswith('tests/') for p in expected),
        'installed_PyYAML': importlib.metadata.version('PyYAML'),
        'actual_extension_path': str(extension),
        'actual_extension_sha256': hashlib.sha256(extension.read_bytes()).hexdigest(),
        'execution_context': 'complete-final-runtime' if args.final_runtime else 'separate-SDK-stage',
        'immutable_runtime_image_id': image_id,
        'actual_Python_executable_sha256': hashlib.sha256(Path(sys.executable).resolve().read_bytes()).hexdigest(),
        'isolated_test_tool_bundle_file_sha256': bundle_hashes,
        'actual_LibYAML_version': yaml._yaml.get_version_string(),
        'installed_distributions': {d.metadata['Name']: d.version for d in importlib.metadata.distributions()},
        'results': results, 'original_default_suite_counts': counts,
        'original_collection_and_assertions_unchanged': True,
        'upstream_original_default_collection_limits_preserved': True,
        'original_default_suite_and_native_smoke_qualified': qualified,
        'full_platform_large_file_or_whole_HAI_acceptance': False}
    (reports / 'qualification.json').write_text(json.dumps(record, indent=2) + '\n')
    if not qualified:
        raise SystemExit('Original publisher suite/native smoke did not qualify')


if __name__ == '__main__':
    main()
