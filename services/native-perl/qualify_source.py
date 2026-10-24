"""Build unchanged publisher Perl only inside the disposable hosted source SDK."""
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
            or os.environ.get('HAI_DISPOSABLE_PERL_SOURCE_SDK') != '1'
            or os.environ.get('HAI_HOSTED_SOURCE_QUALIFICATION') != 'github-hosted'):
        raise RuntimeError('Use the dedicated non-root hosted Perl SDK')
    compiler = subprocess.check_output(['gcc', '-dumpfullversion'], text=True).strip()
    if platform.python_version() != '3.15.1' or compiler != '16.2.0':
        raise RuntimeError('Unexpected source SDK interpreter or compiler')
    digest = lambda raw: hashlib.sha256(raw).hexdigest()
    inputs, work, output = Path('/inputs'), Path('/build'), Path('/outputs')
    spec = json.loads((inputs/'sources.json').read_text(encoding='utf-8'))
    (work/'home').mkdir()
    (work/'tmp').mkdir()
    for item in spec['inputs']:
        raw = (inputs/item['name']).read_bytes()
        if len(raw) != item['bytes'] or digest(raw) != item['sha256']:
            raise ValueError('Publisher archive identity changed')
    with tarfile.open(inputs/'perl-5.44.0.tar.xz', mode='r:xz') as archive:
        members = archive.getmembers()
        if len(members) > 10000 or sum(m.size for m in members) > 512*1024*1024:
            raise ValueError('Unexpected source archive bounds')
        archive.extractall(work, filter='data')
    source = work/spec['archive_root']
    original = {p.relative_to(source).as_posix(): digest(p.read_bytes())
                for p in source.rglob('*') if p.is_file() and not p.is_symlink()}
    cases = {n: h for n, h in original.items()
             if any(part in {'test', 'tests', 't'} for part in Path(n).parts)}
    if len(cases) != spec['source_case_helper_file_count']:
        raise ValueError('Publisher test and helper inventory changed')
    tar_module = source/'cpan/Archive-Tar/lib/Archive/Tar.pm'
    if digest(tar_module.read_bytes()) != spec['bundled_Archive_Tar']['source_sha256']:
        raise ValueError('Publisher Archive::Tar source changed')
    environment = {
        'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/build/home',
        'TMPDIR': '/build/tmp', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
        'TZ': 'UTC', 'LD_LIBRARY_PATH': '/usr/local/lib64:/usr/local/lib',
        'TEST_JOBS': '2',
    }
    report = {
        'upstream_version': spec['version'], 'UID': os.getuid(),
        'Python': platform.python_version(), 'GCC': compiler,
        'original_source_file_sha256': original,
        'original_case_helper_file_sha256': cases,
        'case_helper_files_are_not_test_execution_counts': True,
        'commands': [], 'source_inputs': spec['inputs'],
        'publisher_sources_patched': False, 'Debian_recipe_used': False,
        'PERL_BUILD_PACKAGING_enabled': False, 'case_subset_selected': False,
        'application_cases_authored': False, 'operator_or_runtime_installation': False,
        'Debian_candidate_packages_built': False, 'PGP_signature_verified': False,
        'full_original_upstream_case_acceptance': False,
        'complete_image_ABI_or_deployment_acceptance': False,
    }

    def run(name, command, timeout):
        start = time.monotonic()
        log = output/(name+'.log')
        with log.open('wb') as handle:
            result = subprocess.run(command, cwd=source, env=environment,
                                    stdout=handle, stderr=subprocess.STDOUT, timeout=timeout)
        report['commands'].append({'name': name, 'argv': command,
                                   'returncode': result.returncode,
                                   'seconds': round(time.monotonic()-start, 2),
                                   'log_sha256': digest(log.read_bytes())})
        if result.returncode:
            raise RuntimeError(name+' failed; inspect its retained original log')
        return log.read_text(encoding='utf-8', errors='replace')

    try:
        run('configure', ['sh', 'Configure', '-des', '-Dcc=gcc',
                          '-Dprefix=/opt/hai-perl', '-Dusethreads', '-Duseshrplib'], 600)
        run('compile', ['make', '-j2'], 1500)
        suite = run('original-upstream-suite', ['make', 'test_harness'], 2100)
        summaries = re.findall(r'(?m)^Files=(\d+), Tests=(\d+),.*$', suite)
        if not summaries or 'All tests successful.' not in suite:
            raise RuntimeError('Upstream harness did not provide a successful full-invocation summary')
        markers = [line for line in suite.splitlines()
                   if re.search(r'\b(?:skip|skipped|todo|expected failure)\b', line, re.I)]
        report['upstream_harness_file_count'] = int(summaries[-1][0])
        report['upstream_harness_assertion_count'] = int(summaries[-1][1])
        report['upstream_skip_TODO_or_expected_failure_marker_lines'] = markers
        report['supported_SDK_original_harness_command_passed'] = True
        report['full_original_upstream_case_acceptance'] = not markers
        run('install-private-SDK-prefix', ['make', 'install'], 600)
        probe = run('installed-prefix-inventory', [
            '/opt/hai-perl/bin/perl', '-MConfig', '-MJSON::PP', '-MArchive::Tar',
            '-e', 'print JSON::PP::encode_json({version=>$Config{version},'
            'ccversion=>$Config{ccversion},archname=>$Config{archname},'
            'useithreads=>$Config{useithreads},useshrplib=>$Config{useshrplib},'
            'use64bitint=>$Config{use64bitint},archive_tar=>$Archive::Tar::VERSION,'
            'archive_tar_path=>$INC{"Archive/Tar.pm"}});'
        ], 60)
        inventory = json.loads(probe)
        if (inventory['version'] != '5.44.0' or str(inventory['archive_tar']) != '3.12'
                or inventory['useithreads'] != 'define' or inventory['useshrplib'] != 'true'
                or inventory['use64bitint'] != 'define'
                or not inventory['archive_tar_path'].startswith('/opt/hai-perl/')):
            raise RuntimeError('Installed private-prefix identity or feature profile changed')
        report['installed_SDK_prefix_inventory'] = inventory
        prefix = Path('/opt/hai-perl')
        report['installed_prefix_regular_file_sha256'] = {
            p.relative_to(prefix).as_posix(): digest(p.read_bytes())
            for p in prefix.rglob('*') if p.is_file() and not p.is_symlink()}
        report['installed_prefix_symlinks'] = {
            p.relative_to(prefix).as_posix(): os.readlink(p)
            for p in prefix.rglob('*') if p.is_symlink()}
        shutil.make_archive(str(output/'private-SDK-prefix'), 'gztar', prefix.parent, prefix.name)
    finally:
        changed = [n for n, value in original.items()
                   if not (source/n).is_file() or digest((source/n).read_bytes()) != value]
        changed_cases = [n for n in changed if n in cases]
        report['original_source_files_changed'] = changed
        report['original_case_or_helper_files_changed'] = changed_cases
        report['output_regular_file_sha256'] = {
            p.name: digest(p.read_bytes()) for p in output.iterdir()
            if p.is_file() and p.name != 'qualification.json'}
        report['cgroup_process_counts_at_exit'] = {
            n: (Path('/sys/fs/cgroup')/n).read_text().strip()
            for n in ['pids.current', 'pids.peak', 'pids.max']
            if (Path('/sys/fs/cgroup')/n).is_file()}
        (output/'qualification.json').write_text(json.dumps(report, indent=2)+'\n', encoding='utf-8')
        if changed_cases:
            raise RuntimeError('An original upstream case/helper changed during the source build')


if __name__ == '__main__':
    main()
