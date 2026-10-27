"""Record the actual isolated candidate, including native and bundled versions."""
from pathlib import Path
import hashlib
import importlib.metadata
import importlib.util
import json
import os
import platform
import sqlite3
import ssl
import sys
import sysconfig


def main():
    if (sys.platform != 'linux' or os.geteuid() != 10001
            or Path(__file__).resolve() != Path('/review/runtime_inventory.py')
            or os.environ.get('HAI_RUNTIME_INVENTORY') != '1'):
        raise RuntimeError('Use only the offline read-only candidate inventory')
    context = ssl.create_default_context()
    database = sqlite3.connect(':memory:')
    actual = {'python': platform.python_version(), 'openssl': ssl.OPENSSL_VERSION,
              'sqlite': sqlite3.sqlite_version,
              'sqlite_query': database.execute('select sqlite_version()').fetchone()[0],
              'ca_roots': len(context.get_ca_certs())}
    database.close()
    if (actual['python'] != '3.15.1' or not actual['openssl'].startswith('OpenSSL 4.1.0 ')
            or actual['sqlite'] != '3.54.1' or actual['sqlite_query'] != '3.54.1'
            or actual['ca_roots'] < 1):
        raise RuntimeError('The actual native runtime does not match the pinned source target')
    bundled = Path(sysconfig.get_path('stdlib')) / 'ensurepip/_bundled'
    if importlib.util.find_spec('pip') is not None or list(bundled.glob('*.whl')):
        raise RuntimeError('The immutable runtime still contains its package installer')
    native = Path(sysconfig.get_config_var('DESTSHARED'))
    modules = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
               for p in sorted(native.glob('*.so')) if p.is_file()}
    distributions = [{'name': d.metadata['Name'], 'version': d.version,
        'metadata_sha256': hashlib.sha256(d.read_text('METADATA').encode()).hexdigest()}
        for d in importlib.metadata.distributions()]
    provenance = json.loads(Path('/usr/share/doc/hai-native/provenance.json').read_text())
    signing = json.loads(Path('/usr/share/doc/hai-native/signing-provenance.json').read_text())
    if provenance['build_uid'] != 10001 or signing['signature_checks_bypassed']:
        raise RuntimeError('Native build/signature provenance changed')
    for filename, record in provenance['cpython']['modules'].items():
        if modules.get(str(native / filename)) != record['sha256']:
            raise RuntimeError('Native CPython extension differs from its source build')
    result = {'actual': actual, 'all_native_extension_sha256': modules,
        'python_executable_sha256': hashlib.sha256(Path(sys.executable).read_bytes()).hexdigest(),
        'stdlib_module_names': sorted(sys.stdlib_module_names),
        'python_distributions': distributions, 'native_provenance': provenance,
        'signing_provenance': signing, 'uid': os.geteuid(),
        'operator_database_accessed': False, 'all_components_current': False,
        'full_CPython_or_vendor_suite_acceptance': False, 'deployment_acceptance': False}
    if not any(d['name'].lower() == 'cryptography' for d in distributions):
        raise RuntimeError('The locked cryptography package is absent')
    from cryptography.hazmat.backends.openssl.backend import backend
    result['cryptography_openssl'] = backend.openssl_version_text()
    if not result['cryptography_openssl'].startswith('OpenSSL 4.1.0 '):
        raise RuntimeError('Cryptography must also use the current pinned OpenSSL provider')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
