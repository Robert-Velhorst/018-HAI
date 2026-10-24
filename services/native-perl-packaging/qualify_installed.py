"""Use normal APT and all published Debian installed-package tests in a testbed."""
from pathlib import Path
import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import time


def main():
    if (sys.platform != 'linux' or os.getuid() != 0
            or os.environ.get('HAI_DISPOSABLE_PERL_INSTALLED_TESTBED') != '1'
            or os.environ.get('HAI_HOSTED_SOURCE_QUALIFICATION') != 'github-hosted'):
        raise RuntimeError('Only the disposable installed-package testbed may run this profile')
    if platform.python_version() != '3.15.1':
        raise RuntimeError('Unexpected testbed interpreter')
    inputs, candidates, output = Path('/inputs'), Path('/candidates'), Path('/outputs')
    digest = lambda raw: hashlib.sha256(raw).hexdigest()
    spec = json.loads((inputs/'sources.json').read_text(encoding='utf-8'))
    producer = json.loads((candidates/'qualification.json').read_text(encoding='utf-8'))
    if (producer.get('source_inputs') != spec['inputs']
            or not producer.get('normal_dpkg_source_signature_and_strong_checksum_gate_passed')
            or not producer.get('normal_dependency_checks_and_complete_binary_build_passed')
            or producer.get('unreviewed_case_helper_changes') != []
            or producer.get('published_recipe_files_changed_during_build') != []):
        raise ValueError('Complete normally verified package build is required first')
    for item in spec['inputs']:
        raw = (inputs/item['name']).read_bytes()
        if len(raw) != item['bytes'] or digest(raw) != item['sha256']:
            raise ValueError('Pinned source identity changed')
    packages = producer['candidate_packages']
    if ({p['package'] for p in packages} != set(spec['expected_binary_packages'])
            or len(packages) != len(spec['expected_binary_packages'])):
        raise ValueError('Require the complete candidate family, with no substituted package')
    paths = []
    for item in packages:
        if Path(item['name']).name != item['name'] or item['version'] != spec['version']:
            raise ValueError('Unexpected candidate package identity')
        path = candidates/item['name']
        if digest(path.read_bytes()) != item['sha256']:
            raise ValueError('Candidate archive differs from verified build')
        paths.append(str(path))
    abi_spec = json.loads(Path('/abi-inputs/abi-sources.json').read_text())
    abi_raw = Path('/abi-candidates/abi-qualification.json').read_bytes()
    abi = json.loads(abi_raw)
    if (not abi.get('all_source_and_build_gates_passed')
            or abi.get('original_recipes_or_cases_changed') is not False
            or abi.get('sources') != abi_spec['sources']
            or abi.get('producer_receipt_sha256') != digest((candidates/'qualification.json').read_bytes())
            or not set(abi_spec['expected_binary_packages']) <= {p['package'] for p in abi['candidate_packages']}):
        raise ValueError('Require both normally built target-ABI modules from these exact candidates')
    for item in abi['candidate_packages']:
        if Path(item['name']).name != item['name']:
            raise ValueError('Unexpected ABI archive path')
        path = Path('/abi-candidates')/item['name']
        if digest(path.read_bytes()) != item['sha256']:
            raise ValueError('ABI package archive changed')
        paths.append(str(path))
    Path('/build/home').mkdir(parents=True, exist_ok=True)
    Path('/build/tmp').mkdir(parents=True, exist_ok=True)
    environment = {
        'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/build/home',
        'TMPDIR': '/build/tmp', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
        'TZ': 'UTC', 'DEBIAN_FRONTEND': 'noninteractive',
        'LD_LIBRARY_PATH': '/usr/local/lib64:/usr/local/lib',
    }
    expected = {'hello', 'perl-base-self-contained', 'compression', 'db', 'gdbm',
                'ndbm', 'storable', 'embed', 'control', 'released_versions',
                'syntax', 'verify-configure'}
    report = {
        'scope': 'Normal installation and all published Debian autopkgtests on one disposable amd64 testbed',
        'version': spec['version'], 'candidate_packages': packages,
        'producer_receipt_sha256': digest((candidates/'qualification.json').read_bytes()),
        'abi_receipt_sha256': digest(abi_raw), 'abi_candidate_packages': abi['candidate_packages'],
        'commands': [], 'manager_UID': os.getuid(), 'test_user': 'source-build',
        'normal_installability_passed': False,
        'normal_dependency_and_complete_family_install_passed': False,
        'all_declared_installed_package_tests_passed': False,
        'expected_published_test_names': sorted(expected),
        'original_tests_or_control_overridden': False,
        'dependency_or_signature_or_privilege_checks_bypassed': False,
        'operator_server_or_application_image_installed': False,
        'all_reverse_dependency_ABI_platform_or_image_security_acceptance': False,
    }

    def run(name, command, timeout):
        entry = {'name': name, 'argv': command}
        report['commands'].append(entry)
        log = output/(name+'.log')
        started = time.monotonic()
        try:
            with log.open('wb') as handle:
                result = subprocess.run(command, cwd='/build', env=environment,
                                        stdout=handle, stderr=subprocess.STDOUT, timeout=timeout)
            entry['returncode'] = result.returncode
            if result.returncode:
                raise RuntimeError(name+' failed; retain normal tool diagnostics')
            return log.read_text(encoding='utf-8', errors='replace')
        except subprocess.TimeoutExpired:
            entry['timed_out'] = True
            raise
        finally:
            entry['seconds'] = round(time.monotonic()-started, 2)
            if log.exists():
                entry['log_sha256'] = digest(log.read_bytes())

    try:
        report['initial_installed_package_versions'] = run(
            'initial-package-inventory', ['dpkg-query', '-W', '-f=${binary:Package}\t${Version}\n'], 30)
        # This normal resolver check must succeed without removing SDK packages.
        # No force, ignore-depends, unsigned-repository or alternative-root mode.
        run('normal-installability', ['apt-get', '--simulate', '--no-remove', 'install', *paths], 180)
        report['normal_installability_passed'] = True
        run('normal-complete-family-install', ['apt-get', '-y', '--no-remove', 'install', *paths], 600)
        installed = run('candidate-dpkg-status', ['dpkg-query', '-W',
                        '-f=${binary:Package}\t${Version}\t${db:Status-Status}\n',
                        *sorted(spec['expected_binary_packages'])], 30)
        statuses = [line.split('\t') for line in installed.splitlines()]
        if (len(statuses) != len(packages)
                or any(len(row) != 3 or row[1:] != [spec['version'], 'installed'] for row in statuses)
                or {row[0].split(':')[0] for row in statuses} != set(spec['expected_binary_packages'])):
            raise RuntimeError('Normal package manager did not configure the complete exact family')
        if run('dpkg-audit', ['dpkg', '--audit'], 30).strip():
            raise RuntimeError('Package database reports incomplete configurations')
        report['normal_dependency_and_complete_family_install_passed'] = True
        abi_status = run('ABI-dpkg-status', ['dpkg-query', '-W',
                         '-f=${binary:Package}\t${Version}\t${db:Status-Status}\n',
                         *sorted(abi_spec['expected_binary_packages'])], 30)
        actual = {row[0].split(':')[0]: row[1:] for row in
                  (line.split('\t') for line in abi_status.splitlines()) if len(row) == 3}
        expected_versions = {source['source']: [source['version'], 'installed'] for source in abi_spec['sources']}
        if actual != expected_versions:
            raise RuntimeError('Both exact target-ABI modules must be normally configured')
        report['normal_ABI_module_installation_passed'] = True
        report['installed_perl_config'] = run('installed-perl-config', ['/usr/bin/perl', '-V'], 30)
        run('all-published-installed-cases', ['autopkgtest', '--user=source-build',
            '--no-built-binaries', '--timeout=2400', '--output-dir=/outputs/autopkgtest',
            '--summary=/outputs/autopkgtest.summary', *paths,
            str(inputs/'perl_5.44.0-2.dsc'), '--', 'null'], 2520)
        summary = (output/'autopkgtest.summary').read_text(encoding='utf-8')
        rows = [re.fullmatch(r'(\S+)\s+(PASS|FAIL|SKIP|FLAKY)(?:\s+.*)?', line)
                for line in summary.splitlines() if line.strip()]
        if (not rows or not all(rows) or len(rows) != len(expected)
                or {row[1] for row in rows} != expected or any(row[2] != 'PASS' for row in rows)):
            raise RuntimeError('Full unchanged published case acceptance remains incomplete')
        report['published_case_outcomes'] = {row[1]: row[2] for row in rows}
        report['all_declared_installed_package_tests_passed'] = True
    finally:
        report['output_file_sha256'] = {
            path.relative_to(output).as_posix(): digest(path.read_bytes())
            for path in output.rglob('*') if path.is_file() and path.name != 'installed-qualification.json'}
        (output/'installed-qualification.json').write_text(json.dumps(report, indent=2)+'\n', encoding='utf-8')


if __name__ == '__main__':
    main()
