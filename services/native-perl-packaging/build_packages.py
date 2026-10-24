"""Build the complete signed-source Perl package recipe in the hosted SDK."""
from pathlib import Path
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import time


def main():
    if (sys.platform != 'linux' or os.getuid() != 10001
            or os.environ.get('HAI_DISPOSABLE_PERL_PACKAGE_SDK') != '1'
            or os.environ.get('HAI_HOSTED_SOURCE_QUALIFICATION') != 'github-hosted'):
        raise RuntimeError('Use only the dedicated non-root hosted package SDK')
    compiler = subprocess.check_output(['/usr/local/bin/gcc', '-dumpfullversion'], text=True).strip()
    machine = subprocess.check_output(['/usr/local/bin/gcc', '-dumpmachine'], text=True).strip()
    if platform.python_version() != '3.15.1' or compiler != '16.2.0' or machine != 'x86_64-linux-gnu':
        raise RuntimeError('Unexpected SDK interpreter, compiler or native target')
    inputs, work, output = Path('/inputs'), Path('/build'), Path('/outputs')
    spec = json.loads((inputs/'sources.json').read_text(encoding='utf-8'))
    digest = lambda raw: hashlib.sha256(raw).hexdigest()
    for item in spec['inputs']:
        raw = (inputs/item['name']).read_bytes()
        if len(raw) != item['bytes'] or digest(raw) != item['sha256']:
            raise ValueError('Pinned source input changed')
    (work/'home').mkdir()
    (work/'tmp').mkdir()
    environment = {
        'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/build/home',
        'TMPDIR': '/build/tmp', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'TZ': 'UTC',
        'LD_LIBRARY_PATH': '/usr/local/lib64:/usr/local/lib',
        'DEB_BUILD_OPTIONS': 'parallel=2', 'TEST_JOBS': '2',
        # These are supported variables in the published config.debian recipe.
        'CC': '/usr/local/bin/gcc', 'CPP': '/usr/local/bin/cpp', 'LD': '/usr/local/bin/gcc',
    }
    report = {
        'source_package': spec['source_package'], 'version': spec['version'],
        'UID': os.getuid(), 'GCC': compiler, 'compiler_target': machine,
        'Python': platform.python_version(), 'source_inputs': spec['inputs'],
        'commands': [], 'normal_recipe_patched_test_tree_is_unchanged_publisher_tree': False,
        'complete_original_case_or_platform_acceptance': False,
        'runtime_operator_or_application_image_installation': False,
        'complete_dependency_ABI_CLI_or_image_security_acceptance': False,
        'new_source_patches_or_application_test_cases_authored': False,
        'source_input_OpenPGP_verification_required': True,
        'output_candidate_packages_signed': False,
    }
    report['actual_SDK_package_tool_versions'] = subprocess.check_output(
        ['dpkg-query', '-W', '-f=${binary:Package}\t${Version}\n',
         'dpkg-dev', 'dist', 'debhelper', 'gpg', 'gpgv', 'debian-keyring'], text=True)
    report['official_SDK_keyring_file_sha256'] = {
        p.name: digest(p.read_bytes()) for p in Path('/usr/share/keyrings').glob('debian-*.gpg')
        if p.is_file()}

    def run(name, command, cwd, timeout):
        log = output/(name+'.log')
        start = time.monotonic()
        entry = {'name': name, 'argv': command}
        report['commands'].append(entry)
        try:
            with log.open('wb') as handle:
                result = subprocess.run(command, cwd=cwd, env=environment,
                                        stdout=handle, stderr=subprocess.STDOUT, timeout=timeout)
            entry['returncode'] = result.returncode
            if result.returncode:
                raise RuntimeError(name+' failed; inspect the retained normal-tool log')
            return log.read_text(encoding='utf-8', errors='replace')
        except subprocess.TimeoutExpired:
            entry['timed_out'] = True
            raise
        finally:
            entry['seconds'] = round(time.monotonic()-start, 2)
            if log.is_file():
                entry['log_sha256'] = digest(log.read_bytes())

    renewal_path = Path('/usr/share/doc/hai-perl-keyring/renewal-proof.json')
    renewal = json.loads(renewal_path.read_text(encoding='utf-8'))
    if (renewal['existing_primary_fingerprint'] != spec['existing_official_primary_fingerprint']
            or digest(Path('/usr/share/keyrings/debian-keyring.gpg').read_bytes()) != renewal['updated_official_keyring_sha256']
            or digest(Path('/usr/share/keyrings/hai-perl-original-debian-keyring.gpg').read_bytes()) != renewal['original_official_keyring_sha256']
            or not renewal['original_and_renewed_primary_identity_sets_equal']):
        raise ValueError('SDK-only official certificate renewal proof changed')
    report['SDK_only_existing_certificate_renewal'] = renewal

    original = {}
    baseline = {}
    source = work/spec['archive_root']
    try:
        with tarfile.open(inputs/'perl_5.44.0.orig.tar.xz', mode='r:xz') as archive:
            original = {m.name.removeprefix(spec['archive_root']+'/'): digest(archive.extractfile(m).read())
                        for m in archive.getmembers() if m.isfile()}
        cases = {n: h for n, h in original.items()
                 if any(p in {'test', 'tests', 't'} for p in Path(n).parts)}
        if len(cases) != spec['source_case_helper_file_count']:
            raise ValueError('Original publisher case/helper inventory changed')
        report['publisher_original_source_file_sha256'] = original
        report['publisher_original_case_helper_file_sha256'] = cases
        run('signed-source-extraction', ['dpkg-source', '--require-valid-signature',
                                        '--require-strong-checksums', '-x',
                                        str(inputs/'perl_5.44.0-2.dsc'), str(source)], work, 180)
        report['normal_dpkg_source_signature_and_strong_checksum_gate_passed'] = True
        baseline = {n: digest((source/n).read_bytes()) for n in original if (source/n).is_file()}
        if not set(cases) <= set(baseline):
            raise ValueError('The published recipe removed an original case/helper')
        recipe = ['debian/rules', 'debian/control', 'debian/config.debian', 'debian/patches/series']
        for item in spec['published_patches']:
            name = 'debian/patches/'+item['file']
            if digest((source/name).read_bytes()) != item['sha256']:
                raise ValueError('Published recipe patch changed')
            recipe.append(name)
        report['published_recipe_file_sha256'] = {n: digest((source/n).read_bytes()) for n in recipe}
        report['published_recipe_original_file_changes_after_extraction'] = {
            n: {'publisher': original[n], 'published_recipe': h}
            for n, h in baseline.items() if h != original[n]}
        report['published_recipe_case_helper_changes_after_extraction'] = {
            n: {'publisher': original[n], 'published_recipe': h}
            for n, h in baseline.items() if n in cases and h != original[n]}
        build_log = run('normal-complete-package-build',
                        ['dpkg-buildpackage', '--build=binary', '--no-sign', '--jobs=2'], source, 4800)
        report['normal_dependency_checks_and_complete_binary_build_passed'] = True
        report['upstream_or_packaging_suite_summaries'] = re.findall(
            r'(?m)^(?:All tests successful\.|Files=\d+, Tests=\d+,.*|Result:.*)$', build_log)
        report['skip_TODO_expected_failure_marker_lines'] = [
            line for line in build_log.splitlines()
            if re.search(r'\b(?:skip|skipped|todo|expected failure)\b', line, re.I)]
        configs = {}
        for flavour in ['static', 'shared', 'debug']:
            path = source/('build-'+flavour)/'config.sh'
            text = path.read_text(encoding='utf-8')
            actual = re.search(r"(?m)^gccversion='([^']*)'", text)
            if not actual or actual[1] != '16.2.0':
                raise RuntimeError('Published flavour used a different compiler: '+flavour)
            destination = output/('config-'+flavour+'.sh')
            shutil.copyfile(path, destination)
            configs[flavour] = {'GCC': actual[1], 'config_sha256': digest(path.read_bytes())}
        report['actual_published_flavour_compiler_configs'] = configs
        packages = []
        for package in sorted(work.glob('*.deb')):
            shutil.copyfile(package, output/package.name)
            control = subprocess.check_output(['dpkg-deb', '--field', str(package)], text=True)
            contents = subprocess.check_output(['dpkg-deb', '--contents', str(package)], text=True)
            (output/(package.name+'.control.txt')).write_text(control, encoding='utf-8')
            (output/(package.name+'.contents.txt')).write_text(contents, encoding='utf-8')
            name = re.search(r'(?m)^Package: (.+)$', control)[1]
            version = re.search(r'(?m)^Version: (.+)$', control)[1]
            if version != spec['version']:
                raise ValueError('Unexpected generated candidate version')
            packages.append({'name': package.name, 'package': name, 'version': version,
                             'sha256': digest(package.read_bytes()),
                             'control_sha256': digest(control.encode()),
                             'contents_sha256': digest(contents.encode())})
        if not set(spec['expected_binary_packages']) <= {p['package'] for p in packages}:
            raise RuntimeError('The complete published package family was not generated')
        report['candidate_packages'] = packages
        for pattern in ['*.changes', '*.buildinfo']:
            for path in work.glob(pattern):
                shutil.copyfile(path, output/path.name)
        # No package installation follows this build. Review all dependencies,
        # reverse dependencies and original installed-case behavior separately.
    finally:
        # Preserve partial candidates as evidence even if a later guard fails.
        # Their presence never sets a qualification flag.
        for package in work.glob('*.deb'):
            if package.is_file() and not package.is_symlink():
                shutil.copyfile(package, output/package.name)
        if baseline:
            after = {n: digest((source/n).read_bytes()) for n in original if (source/n).is_file()}
            report['postbuild_original_path_file_sha256'] = after
            unexpected_cases = [n for n in report['publisher_original_case_helper_file_sha256']
                                if after.get(n) not in {original[n], baseline.get(n)}]
            report['unreviewed_case_helper_changes'] = unexpected_cases
            recipe_changes = [n for n, h in report.get('published_recipe_file_sha256', {}).items()
                              if not (source/n).is_file() or digest((source/n).read_bytes()) != h]
            report['published_recipe_files_changed_during_build'] = recipe_changes
        else:
            unexpected_cases, recipe_changes = [], []
        report['output_file_sha256'] = {p.name: digest(p.read_bytes()) for p in output.iterdir()
                                       if p.is_file() and p.name != 'qualification.json'}
        report['cgroup_process_counts_at_exit'] = {
            n: (Path('/sys/fs/cgroup')/n).read_text().strip()
            for n in ['pids.current', 'pids.peak', 'pids.max'] if (Path('/sys/fs/cgroup')/n).is_file()}
        (output/'qualification.json').write_text(json.dumps(report, indent=2)+'\n', encoding='utf-8')
        if unexpected_cases or recipe_changes:
            raise RuntimeError('Original cases or published recipe changed outside the pinned source profile')


if __name__ == '__main__':
    main()
