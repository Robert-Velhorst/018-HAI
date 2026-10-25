"""Compile the pinned native clients in a separate restricted musl SDK.

The Debian entrypoint and guards are unchanged. Packaging is deferred to a
separate root-owned metadata stage; native compilation always uses UID10001.
"""
from pathlib import Path
import hashlib
import json
import os
import shutil
import subprocess
import sys
import sysconfig
import tempfile
import importlib.util

shared = None


def payload_package(root, name, version, origin, architecture):
    target = shared.OUTPUT / 'payloads' / name
    for path in root.rglob('*'):
        if path.is_symlink():
            raise RuntimeError('Native payload roots must contain no symlinks')
    shutil.copytree(root, target)
    files = {p.relative_to(target).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(target.rglob('*')) if p.is_file()}
    record = {'name': name, 'version': version + '-r0', 'origin': 'sqlite' if origin == 'sqlite3' else origin,
              'architecture': architecture, 'payload_root': 'payloads/' + name,
              'files': files, 'compilation_uid': os.geteuid()}
    (shared.OUTPUT / (name + '.json')).write_text(json.dumps(record, indent=2) + '\n')
    return record


def main():
    global shared
    if (sys.platform != 'linux' or os.geteuid() != 10001
            or Path(__file__).resolve().parent != Path('/native-build')
            or os.environ.get('HAI_DISPOSABLE_ALPINE_SDK') != '1'
            or not Path('/etc/alpine-release').read_text().strip().startswith('3.24.')
            or not str(sysconfig.get_config_var('HOST_GNU_TYPE')).endswith('musl')):
        raise RuntimeError('Use only the explicitly marked non-root Alpine3.24 native SDK')
    # Keep Python isolated mode; load only the exact reviewed sibling module
    # after validating the SDK boundary, rather than adding import search paths.
    sibling = Path('/native-build/build_runtime.py')
    if not sibling.is_file() or sibling.is_symlink():
        raise RuntimeError('Missing exact shared native build helper')
    spec = importlib.util.spec_from_file_location('hai_shared_native_build', sibling)
    shared = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(shared)
    manifest = json.loads((shared.ROOT / 'sources.json').read_text())
    profile = json.loads((shared.ROOT / 'profile.json').read_text())
    if (profile != {'python': '3.15.1'} or sys.version.split()[0] != '3.15.1'
            or manifest['prefix'] != '/opt/hai-native'
            or manifest['openssl']['version'] != '4.1.0'
            or manifest['sqlite']['version'] != '3.54.1'):
        raise RuntimeError('Alpine SDK must retain the complete current native profile')
    for path in [shared.PREFIX, shared.OUTPUT, shared.INSTALL]:
        if path.exists() or path.is_symlink():
            raise RuntimeError('Native SDK output must be absent before compilation')
    shared.OUTPUT.mkdir()
    architecture = subprocess.check_output(['apk', '--print-arch'], text=True).strip()
    if architecture != 'x86_64':
        raise RuntimeError('This candidate qualifies only the selected x86_64 target')
    with tempfile.TemporaryDirectory(prefix='hai-musl-sources-') as temporary:
        folder = Path(temporary)
        openssl = shared.extract_source(manifest['openssl'], folder / 'openssl')
        sqlite = shared.extract_source(manifest['sqlite'], folder / 'sqlite')
        cpython = shared.extract_source(manifest['cpython']['3.15.1'], folder / 'cpython')
        tls = shared.build_openssl(manifest['openssl'], openssl, folder, architecture, packager=payload_package)
        database = shared.build_sqlite(manifest['sqlite'], sqlite, folder, architecture, packager=payload_package)
        python = shared.rebuild_python(manifest['cpython']['3.15.1'], cpython)
    env = {**os.environ, 'LD_LIBRARY_PATH': str(shared.SDK_PREFIX / 'lib'),
           'OPENSSL_MODULES': str(shared.SDK_PREFIX / 'lib/ossl-modules'),
           'SSL_CERT_FILE': '/etc/ssl/certs/ca-certificates.crt', 'SSL_CERT_DIR': '/etc/ssl/certs'}
    probe = json.loads(subprocess.check_output([sys.executable, '-I', '-c',
        "import sys;sys.path.insert(0,'/native-build/out/lib-dynload');"
        "import hashlib,json,ssl,sqlite3;db=sqlite3.connect(':memory:');"
        "print(json.dumps({'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,"
        "'query':db.execute('select sqlite_version()').fetchone()[0],"
        "'ca_roots':len(ssl.create_default_context().get_ca_certs()),"
        "'sha256':hashlib.sha256(b'hai-native-runtime').hexdigest()}))"], env=env, text=True, timeout=30))
    if (not probe['openssl'].startswith('OpenSSL 4.1.0 ') or probe['sqlite'] != '3.54.1'
            or probe['query'] != '3.54.1' or probe['ca_roots'] < 1
            or probe['sha256'] != hashlib.sha256(b'hai-native-runtime').hexdigest()):
        raise RuntimeError('The actual musl native-runtime probe does not match its source profile')
    result = {'profile': profile, 'architecture': architecture, 'build_uid': os.geteuid(),
              'openssl': tls, 'sqlite': database, 'cpython': python, 'runtime': probe,
              'compiler': subprocess.check_output(['cc', '--version'], text=True).splitlines()[0],
              'original_SDK_modules_modified': False, 'operator_database_opened': False,
              'full_upstream_vendor_suite_acceptance': False}
    (shared.OUTPUT / 'provenance.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
