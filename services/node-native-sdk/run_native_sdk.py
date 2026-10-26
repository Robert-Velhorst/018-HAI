"""Run unmodified public Node source and cases only in the hosted native SDK."""
from pathlib import Path
import ast
import hashlib
import importlib.util
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import time
import traceback
import urllib.request


ROOT = Path('/node-build')
INPUTS = Path('/node-sdk-inputs')
REPORTS = Path('/reports')
commands = []


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        while block := stream.read(1024 * 1024):
            value.update(block)
    return value.hexdigest()


def command(label, args, cwd=ROOT, env=None, timeout=600):
    started = time.monotonic()
    print(json.dumps({'starting': label, 'command': args}), flush=True)
    with (REPORTS / (label + '.log')).open('w') as log:
        result = subprocess.run(args, cwd=cwd, env=env, stdout=log,
            stderr=subprocess.STDOUT, timeout=timeout)
    commands.append({'label': label, 'command': args, 'returncode': result.returncode,
        'elapsed_seconds': round(time.monotonic() - started, 3)})
    (REPORTS / 'commands.json').write_text(json.dumps(commands, indent=2) + '\n')
    print(json.dumps(commands[-1]), flush=True)
    if result.returncode:
        raise RuntimeError('Original command failed: ' + label)
    return (REPORTS / (label + '.log')).read_text(errors='replace')


def download(url, target, maximum):
    request = urllib.request.Request(url, headers={'User-Agent': 'HAI-public-native-SDK-review'})
    total = 0
    with urllib.request.urlopen(request, timeout=90) as response, target.open('xb') as output:
        while chunk := response.read(1024 * 1024):
            total += len(chunk)
            if total > maximum:
                raise ValueError('Publisher download exceeded its bound')
            output.write(chunk)


def source_snapshot(source):
    rows = subprocess.check_output(['git', 'ls-tree', '-r', '-z', 'HEAD'], cwd=source)
    result = {}
    for row in rows.split(b'\0'):
        if not row:
            continue
        metadata, raw_name = row.split(b'\t', 1)
        mode, kind, oid = metadata.decode().split()
        if kind != 'blob':
            raise ValueError('Every original publisher payload must be inventoried')
        name = os.fsdecode(raw_name)
        path = source / name
        value = (hashlib.sha256(os.fsencode(os.readlink(path))).hexdigest()
            if path.is_symlink() else digest(path))
        result[name] = {'mode': mode, 'Git_blob': oid, 'checkout_sha256': value}
    if not result or not any(p.startswith('test/') for p in result):
        raise ValueError('The complete original Git source and cases are required')
    subprocess.run(['git', 'diff', '--exit-code', '--'], cwd=source, check=True,
        stdout=subprocess.DEVNULL)
    return result


def inventory(executable, env):
    # Native/provider inventory only; not a new application case.
    script = """const crypto=require('node:crypto');
const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(':memory:');
console.log(JSON.stringify({versions:process.versions,features:process.features,configVariables:process.config.variables,
platform:process.platform,arch:process.arch,sqlite:db.prepare('select sqlite_version() AS version').get().version,
sqliteCompileOptions:db.prepare('pragma compile_options').all().map(r=>r.compile_options).sort(),
ciphers:crypto.getCiphers().sort(),hashes:crypto.getHashes().sort(),curves:crypto.getCurves().sort()}));db.close();"""
    return json.loads(subprocess.check_output([str(executable), '-e', script], env=env, text=True))


def main():
    release = dict(line.split('=', 1) for line in Path('/usr/lib/os-release').read_text().splitlines() if '=' in line)
    if (sys.platform != 'linux' or os.geteuid() != 10001 or os.getegid() != 10001
            or os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted'
            or os.environ.get('HAI_DISPOSABLE_NATIVE_NODE_SDK') != '1'
            or release.get('ID', '').strip('"') != 'ubuntu'
            or release.get('VERSION_ID', '').strip('"') != '26.10'
            or not re.fullmatch('[0-9a-f]{40}', os.environ.get('GITHUB_SHA', ''))):
        raise RuntimeError('Use only the marked non-root hosted Ubuntu26.10 SDK')
    spec = json.loads((INPUTS / 'source-spec.json').read_text())
    if platform.python_version() != spec['expected_python']:
        raise ValueError('Keep the current real Python compiler version')
    for name, expected in [('release-signer.asc', spec['signer_key_sha256']),
            ('active-release-keys.kbx', spec['active_keyring_sha256'])]:
        if digest(INPUTS / name) != expected:
            raise ValueError('Official publisher key bytes differ')
    for name in ['NODE_OPTIONS', 'NODE_SKIP_PLATFORM_CHECK', 'NODE_TLS_REJECT_UNAUTHORIZED',
            'TEST_CI_ARGS', 'CI_SKIP_TESTS', 'FLAKY_TESTS']:
        if name in os.environ:
            raise ValueError('Do not inject runtime, original case or platform bypass settings')
    public_home = ROOT / 'public-keyring'
    public_home.mkdir(mode=0o700)
    env = {**os.environ, 'GNUPGHOME': str(public_home)}
    command('import-publisher-public-key', ['gpg', '--batch', '--import', str(INPUTS / 'release-signer.asc')], env=env)
    source = ROOT / 'node-source'
    source.mkdir()
    command('original-Git-init', ['git', 'init', '-q'], source, env)
    command('original-Git-origin', ['git', 'remote', 'add', 'origin', 'https://github.com/nodejs/node.git'], source, env)
    command('original-Git-fetch', ['git', 'fetch', '--depth=1', 'origin',
        'refs/tags/' + spec['tag'] + ':refs/tags/' + spec['tag']], source, env, timeout=1200)
    signature = command('verify-original-tag', ['git', 'verify-tag', '--raw', spec['tag']], source, env)
    valid = [line.split() for line in signature.splitlines() if line.startswith('[GNUPG:] VALIDSIG ')]
    if not any(spec['tag_signer_fingerprint'] in (line[2], line[-1]) for line in valid):
        raise ValueError('The original tag signer differs from the pinned official key')
    command('original-Git-checkout', ['git', 'checkout', '--detach', spec['commit']], source, env)
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=source, text=True).strip() != spec['commit']:
        raise ValueError('Publisher commit differs')
    if subprocess.check_output(['git', 'rev-parse', spec['tag']], cwd=source, text=True).strip() != spec['tag_object_sha']:
        raise ValueError('Publisher signed tag object differs')
    original = source_snapshot(source)
    (REPORTS / 'original-publisher-source.json').write_text(json.dumps({
        'publisher_commit': spec['commit'], 'signed_tag': spec['tag'], 'payloads': original}, indent=2) + '\n')
    gyp = source / 'deps/sqlite/sqlite.gyp'
    if digest(gyp) != spec['publisher_sqlite_gyp_sha256']:
        raise ValueError('Original SQLite compile profile differs')
    flags = next(t for t in ast.literal_eval(gyp.read_text())['targets'] if t['target_name'] == 'sqlite')['defines']
    if flags != spec['all_original_Node_SQLite_defines']:
        raise ValueError('Keep every original Node SQLite definition')
    shared_spec = importlib.util.spec_from_file_location('hai_public_source_helper', '/native-build/build_runtime.py')
    shared = importlib.util.module_from_spec(shared_spec)
    shared_spec.loader.exec_module(shared)
    manifest = json.loads(Path('/native-build/sources.json').read_text())
    sqlite = shared.extract_source(manifest['sqlite'], ROOT / 'sqlite-source')
    provider = ROOT / 'providers/sqlite'
    sql_env = {**env, 'CFLAGS': ' '.join('-D' + name for name in flags)}
    command('original-SQLite-configure', ['./configure', '--prefix=' + str(provider)], sqlite, sql_env)
    command('original-SQLite-build', ['make', '-j2'], sqlite, sql_env, timeout=1800)
    command('original-SQLite-install', ['make', 'install'], sqlite, sql_env)
    # Normal upstream installation; retain Temporal instead of disabling it.
    command('normal-Rust-toolchain-install', ['rustup', 'toolchain', 'install', spec['rust_toolchain'], '--profile', 'minimal'], env=env, timeout=1800)
    command('normal-Rust-toolchain-select', ['rustup', 'default', spec['rust_toolchain']], env=env)
    command('actual-Rust-compiler', ['rustc', '--version'], env=env)
    checksums = ROOT / 'SHASUMS256.txt'
    signature_path = ROOT / 'SHASUMS256.txt.sig'
    dist = 'https://nodejs.org/dist/' + spec['tag'] + '/'
    download(dist + checksums.name, checksums, 1024 * 1024)
    download(dist + signature_path.name, signature_path, 1024 * 1024)
    command('verify-official-binary-checksums', ['gpgv', '--keyring', str(INPUTS / 'active-release-keys.kbx'), str(signature_path), str(checksums)], env=env)
    filename = 'node-' + spec['tag'] + '-linux-x64.tar.xz'
    entries = dict(line.split(None, 1)[::-1] for line in checksums.read_text().splitlines() if line.strip())
    archive = ROOT / filename
    download(dist + filename, archive, 128 * 1024 * 1024)
    if digest(archive) != entries[filename]:
        raise ValueError('Official baseline archive checksum differs')
    baseline_binary = ROOT / 'official-baseline-node'
    with tarfile.open(archive) as bundle:
        member = bundle.getmember('node-' + spec['tag'] + '-linux-x64/bin/node')
        if not member.isfile():
            raise ValueError('Expected the original official Node executable')
        with bundle.extractfile(member) as data, baseline_binary.open('xb') as output:
            shutil.copyfileobj(data, output)
    baseline_binary.chmod(0o755)
    baseline = inventory(baseline_binary, env)
    (REPORTS / 'official-baseline-native-inventory.json').write_text(json.dumps(baseline, indent=2) + '\n')
    node_env = {**env, 'LD_LIBRARY_PATH': str(provider / 'lib') + ':' + env['LD_LIBRARY_PATH'],
        'LDFLAGS': '-Wl,-rpath,' + str(provider / 'lib') + ' -Wl,-rpath,/opt/hai-native/lib'}
    command('original-Node-configure', ['./configure', '--prefix=' + str(ROOT / 'installed-node'),
        '--with-intl=full-icu', '--shared-openssl',
        '--shared-openssl-includes=/native-build/sdk/opt/hai-native/include',
        '--shared-openssl-libpath=/native-build/sdk/opt/hai-native/lib',
        '--shared-sqlite', '--shared-sqlite-includes=' + str(provider / 'include'),
        '--shared-sqlite-libpath=' + str(provider / 'lib')], source, node_env)
    command('original-Node-build', ['make', '-j2'], source, node_env, timeout=7200)
    executable = source / 'out/Release/node'
    tested_node_sha256 = digest(executable)
    candidate = inventory(executable, node_env)
    (REPORTS / 'actual-native-inventory.json').write_text(json.dumps(candidate, indent=2) + '\n')
    command('original-Node-CI-suite', ['make', '-j2', 'test-ci'], source, node_env, timeout=7200)
    actual = source_snapshot(source)
    if original != actual:
        raise ValueError('Original publisher source, helpers, cases or assertions changed')
    tap = source / 'test.tap'
    if not tap.is_file():
        raise ValueError('Retain the actual original CI TAP report')
    shutil.copyfile(tap, REPORTS / 'original-Node-test.tap')
    raw_cases = tap.read_text(errors='replace')
    if not re.search(r'(?m)^ok\b', raw_cases) or re.search(r'(?m)^not ok\b', raw_cases):
        raise ValueError('Original TAP reports a non-passing case; no flaky or TODO waiver')
    if (candidate['versions']['node'] != spec['tag'].removeprefix('v')
            or candidate['versions']['openssl'] != spec['expected_openssl']
            or candidate['sqlite'] != spec['expected_sqlite']):
        raise ValueError('Actual selected native component versions differ')
    lost = {name: sorted(set(baseline[name]) - set(candidate[name])) for name in ['ciphers', 'hashes', 'curves']}
    missing_sql = sorted({x for x in baseline['sqliteCompileOptions'] if not x.startswith('COMPILER=')}
        - set(candidate['sqliteCompileOptions']))
    feature_names = ['node_use_openssl', 'node_use_sqlite', 'v8_enable_webassembly',
        'v8_enable_temporal_support', 'v8_enable_i18n_support', 'node_use_node_snapshot',
        'node_use_node_code_cache', 'node_use_v8_platform', 'icu_small']
    lost_configuration = [name for name in feature_names
        if baseline['configVariables'].get(name) != candidate['configVariables'].get(name)]
    missing_components = sorted(set(baseline['versions']) - set(candidate['versions']))
    if (any(lost.values()) or missing_sql or lost_configuration or missing_components
            or baseline['features'] != candidate['features']):
        raise ValueError('Original crypto, SQLite or Node features were lost')
    command('original-Node-install', ['make', 'install'], source, node_env, timeout=1200)
    if digest(executable) != tested_node_sha256:
        raise ValueError('The compiled executable changed after the original CI run')
    shutil.copyfile(executable, REPORTS / 'candidate-node')
    shutil.copyfile(source / 'LICENSE', REPORTS / 'Node-LICENSE')
    linkage = command('actual-native-linkage', ['ldd', str(executable)], source, node_env)
    providers = {}
    for name in re.findall(r'=> (/\S+)', linkage):
        path = Path(name).resolve(strict=True)
        providers[str(path)] = digest(path)
    (REPORTS / 'qualification.json').write_text(json.dumps({
        'source_head': os.environ['GITHUB_SHA'], 'publisher_commit': spec['commit'],
        'original_source_payloads_verified_before_after': len(original),
        'all_original_source_cases_helpers_assertions_unchanged': True,
        'all_original_Node_SQLite_defines_preserved': flags,
        'original_Node_CI_command_passed_without_reported_nonpassing_TAP_cases': True,
        'original_upstream_platform_and_skip_policy_unchanged': True,
        'native_versions': candidate['versions'], 'Node_SQLite': candidate['sqlite'],
        'actual_node_executable_sha256': digest(executable), 'actual_linked_provider_sha256': providers,
        'original_features_preserved': True, 'new_application_cases_authored': False,
        'all_other_bundled_components_or_full_OS_SDK_security_acceptance': False,
        'other_platforms_complete_vendor_suites_or_application_deployment_acceptance': False,
        'production_recipe_changed': False}, indent=2) + '\n')


def retain_diagnostics():
    if not REPORTS.is_dir() or sys.platform != 'linux' or os.geteuid() != 10001:
        return
    source = ROOT / 'node-source'
    for path, name in [(source / 'test.tap', 'original-Node-test.tap'),
            (source / 'out/junit/cctest.xml', 'original-cctest.xml'),
            (source / 'LICENSE', 'Node-LICENSE'),
            (Path('/native-build/out/provenance.json'), 'support-native-provenance.json')]:
        if path.is_file() and not path.is_symlink():
            shutil.copyfile(path, REPORTS / name)
    binary = source / 'out/Release/node'
    if binary.is_file() and not binary.is_symlink():
        shutil.copyfile(binary, REPORTS / 'candidate-node')
        env = {**os.environ, 'LD_LIBRARY_PATH': str(ROOT / 'providers/sqlite/lib') + ':' + os.environ.get('LD_LIBRARY_PATH', '')}
        result = subprocess.run(['ldd', str(binary)], text=True, env=env, capture_output=True)
        (REPORTS / 'diagnostic-native-linkage.log').write_text(result.stdout + result.stderr)
        libraries = REPORTS / 'native-libraries'
        libraries.mkdir(exist_ok=True)
        providers = {}
        for name in re.findall(r'=> (/\S+)', result.stdout):
            path = Path(name).resolve(strict=True)
            if path.is_relative_to(ROOT / 'providers') or path.is_relative_to(Path('/opt/hai-native')):
                value = digest(path)
                shutil.copyfile(path, libraries / (value + '-' + path.name))
                providers[str(path)] = value
        (REPORTS / 'diagnostic-native-provider-sha256.json').write_text(json.dumps(providers, indent=2) + '\n')
    if (source / '.git').is_dir():
        result = subprocess.run(['git', 'status', '--porcelain', '--untracked-files=no'],
            cwd=source, text=True, capture_output=True)
        (REPORTS / 'original-source-final-Git-status.log').write_text(result.stdout + result.stderr)
    # Public upstream source remains recoverable by its pinned signed Git tag.
    # Do not archive dependency trees, compiler caches or duplicate source trees.


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        if REPORTS.is_dir():
            (REPORTS / 'failure.json').write_text(json.dumps({
                'error_type': type(exc).__name__, 'error': str(exc),
                'source_head': os.environ.get('GITHUB_SHA'),
                'native_SDK_or_application_acceptance': False}, indent=2) + '\n')
        traceback.print_exc()
        raise SystemExit(1)
    finally:
        retain_diagnostics()
