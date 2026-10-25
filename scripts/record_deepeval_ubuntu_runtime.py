"""Record the actual isolated Ubuntu candidate; never open operator databases."""
from pathlib import Path
import hashlib
import importlib.metadata
import importlib.util
import json
import os
import platform
import shlex
import sqlite3
import ssl
import subprocess
import sys
import sysconfig


def main():
    if (sys.platform!='linux' or os.geteuid()!=10001 or os.getegid()!=10001
            or Path(__file__).resolve()!=Path('/review/runtime_inventory.py')
            or os.environ.get('HAI_RUNTIME_INVENTORY')!='1'):
        raise RuntimeError('Use only the marked offline read-only non-root inventory')
    database=sqlite3.connect(':memory:')
    actual={'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,
        'sqlite':sqlite3.sqlite_version,'sqlite_query':database.execute('select sqlite_version()').fetchone()[0],
        'ca_roots':len(ssl.create_default_context().get_ca_certs())}
    database.close()
    from cryptography.hazmat.backends.openssl.backend import backend
    actual['cryptography_openssl']=backend.openssl_version_text()
    if (actual['python']!='3.15.1' or not actual['openssl'].startswith('OpenSSL 4.1.0 ')
            or not actual['cryptography_openssl'].startswith('OpenSSL 4.1.0 ')
            or actual['sqlite']!='3.54.1' or actual['sqlite_query']!='3.54.1' or actual['ca_roots']<1):
        raise RuntimeError('Every actual native provider must match the source profile')
    bundled=Path(sysconfig.get_path('stdlib'))/'ensurepip/_bundled'
    if importlib.util.find_spec('pip') is not None or list(bundled.glob('*.whl')):
        raise RuntimeError('Package installer or bootstrap wheels remain in final image')
    native=Path(sysconfig.get_config_var('DESTSHARED'))
    modules={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(native.glob('*.so'))}
    provenance=json.loads(Path('/usr/share/doc/hai-native/provenance.json').read_text())
    if provenance['build_uid']!=10001 or modules!=provenance['cpython']['modules']:
        raise RuntimeError('Complete source-built native module identity changed')
    if hashlib.sha256(Path('/usr/local/bin/python3.15').read_bytes()).hexdigest()!=provenance['cpython']['python_executable_sha256']:
        raise RuntimeError('Final Python executable differs from source build')
    release={}
    for line in Path('/usr/lib/os-release').read_text().splitlines():
        if '=' in line:
            key,value=line.split('=',1)
            values=shlex.split(value)
            release[key]=values[0] if values else ''
    if release.get('ID')!='ubuntu' or release.get('VERSION_ID')!='26.10':
        raise RuntimeError('The exact current Ubuntu base is required')
    packages={}
    raw=subprocess.check_output(['dpkg-query','--show','--showformat=${db:Status-Abbrev}\t${binary:Package}\t${Version}\n'],text=True)
    for line in raw.splitlines():
        state,name,version=line.split('\t',2)
        if len(state)>1 and state[1]=='i':
            packages[name]=version
    distributions=[{'name':d.metadata['Name'],'version':d.version,
        'metadata_sha256':hashlib.sha256(d.read_text('METADATA').encode()).hexdigest()}
        for d in importlib.metadata.distributions()]
    versions={d['name'].lower():d['version'] for d in distributions}
    if versions.get('deepeval')!='4.3.1':
        raise RuntimeError('The complete locked public DeepEval engine is required')
    print(json.dumps({'actual':actual,'os_release':release,'installed_OS_packages':packages,
        'python_distributions':distributions,'all_native_extension_sha256':modules,
        'stdlib_module_names':sorted(sys.stdlib_module_names),'native_provenance':provenance,
        'source_file_sha256':{name:hashlib.sha256((Path('/app')/name).read_bytes()).hexdigest()
            for name in ['app.py','test_app.py','requirements.lock']},
        'uid':os.geteuid(),'operator_database_accessed':False,'all_components_current':False,
        'full_CPython_or_vendor_suite_acceptance':False,'deployment_acceptance':False},indent=2))


if __name__=='__main__':
    main()
