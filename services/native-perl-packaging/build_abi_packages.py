"""Build unchanged published XS recipes using normally installed target Perl."""
from pathlib import Path
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import time


def main():
    if (sys.platform != 'linux' or os.getuid() != 0
            or os.environ.get('HAI_DISPOSABLE_PERL_ABI_SDK') != '1'
            or os.environ.get('HAI_HOSTED_SOURCE_QUALIFICATION') != 'github-hosted'
            or platform.python_version() != '3.15.1'):
        raise RuntimeError('Only the disposable hosted ABI bootstrap SDK may run')
    inputs, candidates, output = Path('/abi-inputs'), Path('/candidates'), Path('/abi-outputs')
    digest = lambda raw: hashlib.sha256(raw).hexdigest()
    spec = json.loads((inputs/'abi-sources.json').read_text())
    producer_raw = (candidates/'qualification.json').read_bytes()
    producer = json.loads(producer_raw)
    if (not producer.get('normal_dpkg_source_signature_and_strong_checksum_gate_passed')
            or not producer.get('normal_dependency_checks_and_complete_binary_build_passed')
            or producer.get('unreviewed_case_helper_changes') != []
            or producer.get('published_recipe_files_changed_during_build') != []):
        raise ValueError('Require the normally verified complete Perl build')
    expected = {'libperl-dev', 'libperl5.44', 'perl-base', 'perl-debug', 'perl-doc', 'perl-modules-5.44', 'perl'}
    primary = producer['candidate_packages']
    if len(primary) != 7 or {p['package'] for p in primary} != expected:
        raise ValueError('Require all seven Perl packages')
    paths = []
    for item in primary:
        if Path(item['name']).name != item['name'] or item['version'] != '5.44.0-2':
            raise ValueError('Unexpected primary package identity')
        path = candidates/item['name']
        if digest(path.read_bytes()) != item['sha256']:
            raise ValueError('Primary package archive changed')
        paths.append(str(path))
    for source in spec['sources']:
        for item in source['inputs']:
            if Path(item['name']).name != item['name']:
                raise ValueError('Unexpected source path')
            raw = (inputs/item['name']).read_bytes()
            if len(raw) != item['bytes'] or digest(raw) != item['sha256']:
                raise ValueError('Source input changed')
    for name in ['home', 'tmp', 'sources']:
        path = Path('/build')/name
        path.mkdir(parents=True, exist_ok=True)
        os.chown(path, 10001, 10001)
    environment = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/build/home',
                   'TMPDIR': '/build/tmp', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
                   'TZ': 'UTC', 'DEBIAN_FRONTEND': 'noninteractive',
                   'LD_LIBRARY_PATH': '/usr/lib/x86_64-linux-gnu:/usr/local/lib64:/usr/local/lib'}
    report = {'scope': 'Normal target-Perl installation in a separate build SDK, then both unchanged published XS recipes',
              'producer_receipt_sha256': digest(producer_raw), 'sources': spec['sources'],
              'commands': [], 'candidate_packages': [], 'all_source_and_build_gates_passed': False,
              'source_build_UID': 10001, 'original_recipes_or_cases_changed': False,
              'full_existing_testbed_or_operator_packages_removed': False}

    def run(name, command, cwd='/build', timeout=300, builder=False):
        entry = {'name': name, 'argv': command, 'UID': 10001 if builder else 0}
        report['commands'].append(entry)
        started = time.monotonic()
        log = output/(name+'.log')
        try:
            with log.open('wb') as handle:
                options = {'user': 10001, 'group': 10001, 'extra_groups': []} if builder else {}
                result = subprocess.run(command, cwd=cwd, env=environment, timeout=timeout,
                                        stdout=handle, stderr=subprocess.STDOUT, **options)
            entry['returncode'] = result.returncode
            if result.returncode:
                raise RuntimeError(name+' failed under normal tool policy')
            return log.read_text(errors='replace')
        finally:
            entry['seconds'] = round(time.monotonic()-started, 2)
            if log.exists():
                entry['log_sha256'] = digest(log.read_bytes())

    try:
        keyring_raw = Path('/usr/share/doc/hai-source-keyring-setup/qualification.json').read_bytes()
        keyring = json.loads(keyring_raw)
        if (not keyring.get('normal_authenticated_install_and_owned_keyring_passed')
                or digest(Path(keyring['keyring_path']).read_bytes()) != keyring['keyring_sha256']):
            raise RuntimeError('Require the normally installed official source-signing keyring')
        report['official_source_keyring_receipt_sha256'] = digest(keyring_raw)
        report['official_source_keyring'] = keyring
        runtime_path=Path('/usr/share/doc/hai-gcc-runtime-setup/runtime-qualification.json')
        runtime_raw=runtime_path.read_bytes();runtime=json.loads(runtime_raw)
        if not runtime.get('normal_authenticated_APT_and_exact_package_install_passed'):
            raise RuntimeError('Require the real normally installed GCC 16 runtime packages')
        for item in runtime['owned_runtime_files'].values():
            if digest(Path(item['path']).read_bytes())!=item['sha256']:
                raise RuntimeError('Owned compiler runtime changed')
        report['official_compiler_runtime_receipt_sha256']=digest(runtime_raw)
        report['official_compiler_runtime']=runtime
        run('initial-package-relations', ['dpkg-query', '-W', '-f=${binary:Package}\t${Version}\t${Depends}\t${Provides}\n'])
        run('bootstrap-installability', ['apt-get', '--simulate', '--no-remove', 'install', *paths])
        run('bootstrap-complete-install', ['apt-get', '-y', '--no-remove', 'install', *paths], timeout=600)
        status = run('bootstrap-exact-package-status', ['dpkg-query', '-W',
                     '-f=${binary:Package}\t${Version}\t${db:Status-Status}\n', *sorted(expected)])
        actual = {row[0].split(':')[0]: row[1:] for row in
                  (line.split('\t') for line in status.splitlines()) if len(row) == 3}
        if actual != {name: ['5.44.0-2', 'installed'] for name in expected}:
            raise RuntimeError('The complete exact Perl family must be normally configured')
        installed = run('target-perl-version', ['/usr/bin/perl', '-e', 'print $^V'], builder=True)
        if installed.strip() != 'v5.44.0':
            raise RuntimeError('Modules must use the actual installed target interpreter')
        if run('bootstrap-dpkg-audit', ['dpkg', '--audit']).strip():
            raise RuntimeError('Bootstrap package state is incomplete')
        if run('source-compiler-version', ['/usr/local/bin/gcc', '-dumpfullversion'], builder=True).strip() != '16.2.0':
            raise RuntimeError('Unexpected source compiler')
        if run('source-cxx-version', ['/usr/local/bin/c++', '-dumpfullversion'], builder=True).strip() != '16.2.0':
            raise RuntimeError('Unexpected XS C++ compiler')
        report['published_source_and_case_file_hashes'] = {}
        for source in spec['sources']:
            name = source['source']
            target = Path('/build/sources')/name
            run(name+'-signed-source', ['dpkg-source', '--require-valid-signature', '--require-strong-checksums',
                                      '-x', str(inputs/source['dsc']), str(target)], builder=True)
            protected = {p.relative_to(target).as_posix(): digest(p.read_bytes()) for p in target.rglob('*')
                         if p.is_file() and not p.is_symlink()
                         and (p.name in {'control', 'rules', 'Makefile.PL'}
                              or any(part in {'t', 'test', 'tests'} for part in p.relative_to(target).parts))}
            report['published_source_and_case_file_hashes'][name] = protected
            try:
                run(name+'-complete-build', ['dpkg-buildpackage', '--build=binary', '--no-sign', '--jobs=2'],
                    cwd=target, timeout=900, builder=True)
            finally:
                changed = [n for n, sha in protected.items()
                           if not (target/n).is_file() or digest((target/n).read_bytes()) != sha]
                if changed:
                    report['original_recipes_or_cases_changed'] = True
                    report['unexpected_case_or_recipe_changes'] = changed
                    raise RuntimeError('Published recipe or cases changed during build')
        for path in sorted(Path('/build/sources').glob('*.deb')):
            control = run(path.name+'-control', ['dpkg-deb', '--field', str(path)])
            fields = dict(re.findall(r'(?m)^(Package|Version|Depends): (.+)$', control))
            name = fields['Package']
            if name not in set(spec['expected_binary_packages']) | {n+'-dbgsym' for n in spec['expected_binary_packages']}:
                raise ValueError('Unexpected generated ABI package')
            source_name = name.removesuffix('-dbgsym')
            version = next(s['version'] for s in spec['sources'] if s['source'] == source_name)
            if fields['Version'] != version:
                raise ValueError('Unexpected ABI package version')
            if name in spec['expected_binary_packages'] and 'perlapi-5.44.0' not in fields.get('Depends', ''):
                raise ValueError('Generated dependencies do not target the actual new Perl ABI')
            shutil.copyfile(path, output/path.name)
            report['candidate_packages'].append({'name': path.name, 'package': name, 'version': version,
                                                  'sha256': digest(path.read_bytes()), 'control': fields})
        if not set(spec['expected_binary_packages']) <= {p['package'] for p in report['candidate_packages']}:
            raise RuntimeError('Both rebuilt ABI modules are required')
        report['all_source_and_build_gates_passed'] = True
    finally:
        # Partial archives remain evidence and never set a qualification flag.
        for pattern in ['*.deb', '*.changes', '*.buildinfo']:
            for path in Path('/build/sources').glob(pattern):
                if path.is_file() and not path.is_symlink():
                    shutil.copyfile(path, output/path.name)
        report['output_file_sha256'] = {p.name: digest(p.read_bytes()) for p in output.iterdir()
                                        if p.is_file() and p.name != 'abi-qualification.json'}
        (output/'abi-qualification.json').write_text(json.dumps(report, indent=2)+'\n')


if __name__ == '__main__':
    main()
