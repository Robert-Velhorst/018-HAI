"""Compile complete current CPython/native clients only in the Ubuntu disposable SDK."""
from pathlib import Path
import hashlib
import importlib.util
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile


def main():
    root=Path('/native-build')
    release={}
    for line in Path('/usr/lib/os-release').read_text().splitlines():
        if '=' in line:
            key,value=line.split('=',1)
            values=shlex.split(value)
            release[key]=values[0] if values else ''
    if (sys.platform!='linux' or os.geteuid()!=10001 or os.getegid()!=10001
            or Path(__file__).resolve().parent!=root
            or os.environ.get('HAI_DISPOSABLE_UBUNTU_SDK')!='1'
            or release.get('ID')!='ubuntu' or release.get('VERSION_ID')!='26.10'
            or release.get('RELEASE_TYPE')!='stable'):
        raise RuntimeError('Use only the marked non-root stable Ubuntu26.10 disposable SDK')
    sibling=root/'build_runtime.py'
    if sibling.is_symlink() or not sibling.is_file():
        raise RuntimeError('Missing exact shared source/download/build helper')
    spec=importlib.util.spec_from_file_location('hai_shared_native_build',sibling)
    shared=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(shared)
    manifest=json.loads((root/'sources.json').read_text())
    profile=json.loads((root/'profile.json').read_text())
    if (profile!={'python':'3.15.1'} or manifest['prefix']!='/opt/hai-native'
            or manifest['openssl']['version']!='4.1.0'
            or manifest['sqlite']['version']!='3.54.1'):
        raise RuntimeError('Keep the complete current native source profile')
    for path in [shared.PREFIX,shared.OUTPUT,shared.INSTALL]:
        if path.exists() or path.is_symlink():
            raise RuntimeError('Native outputs must be absent before compilation')
    shared.OUTPUT.mkdir()
    architecture=subprocess.check_output(['dpkg','--print-architecture'],text=True).strip()
    if architecture!='amd64':
        raise RuntimeError('This candidate qualifies only the recorded amd64 architecture')
    with tempfile.TemporaryDirectory(prefix='hai-ubuntu-sources-') as temporary:
        folder=Path(temporary)
        ssl=shared.extract_source(manifest['openssl'],folder/'openssl')
        sqlite=shared.extract_source(manifest['sqlite'],folder/'sqlite')
        python=shared.extract_source(manifest['cpython']['3.15.1'],folder/'cpython')
        tls=shared.build_openssl(manifest['openssl'],ssl,folder,architecture)
        database=shared.build_sqlite(manifest['sqlite'],sqlite,folder,architecture)
        for name in ['sqlite3.h','sqlite3ext.h']:
            shared.copy_regular(sqlite/name,shared.SDK_PREFIX/'include'/name)
        (shared.SDK_PREFIX/'lib/libsqlite3.so').symlink_to('libsqlite3.so.0')
        env={**os.environ,'LD_LIBRARY_PATH':str(shared.SDK_PREFIX/'lib'),
             'CPPFLAGS':'-I'+str(shared.SDK_PREFIX/'include'),
             'LDFLAGS':'-L'+str(shared.SDK_PREFIX/'lib')+' -Wl,-rpath,/opt/hai-native/lib -Wl,-rpath,/usr/local/lib'}
        shared.command(['./configure','--prefix=/usr/local','--enable-shared',
            '--with-ensurepip=install','--with-openssl='+str(shared.SDK_PREFIX),
            '--with-openssl-rpath=/opt/hai-native/lib'],python,env=env)
        shared.command(['make','-j2'],python,env=env,timeout=1800)
        shared.command(['make','install','DESTDIR='+str(shared.INSTALL)],python,env=env,timeout=1200)
        installed=shared.INSTALL/'usr/local'
        executable=installed/'bin/python3.15'
        probe_env={**env,'LD_LIBRARY_PATH':str(installed/'lib')+':'+str(shared.SDK_PREFIX/'lib'),
            'PYTHONHOME':str(installed),'OPENSSL_MODULES':str(shared.SDK_PREFIX/'lib/ossl-modules'),
            'SSL_CERT_FILE':'/etc/ssl/certs/ca-certificates.crt','SSL_CERT_DIR':'/etc/ssl/certs'}
        probe=json.loads(subprocess.check_output([str(executable),'-c',
            "import hashlib,json,platform,sqlite3,ssl,sys;d=sqlite3.connect(':memory:');"
            "print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,"
            "'sqlite':sqlite3.sqlite_version,'sqlite_query':d.execute('select sqlite_version()').fetchone()[0],"
            "'ca_roots':len(ssl.create_default_context().get_ca_certs()),'stdlib_names':sorted(sys.stdlib_module_names),"
            "'sha256':hashlib.sha256(b'hai-native-runtime').hexdigest()}))"],env=probe_env,text=True,timeout=30))
        if (probe['python']!='3.15.1' or not probe['openssl'].startswith('OpenSSL 4.1.0 ')
                or probe['sqlite']!='3.54.1' or probe['sqlite_query']!='3.54.1' or probe['ca_roots']<1
                or probe['sha256']!=hashlib.sha256(b'hai-native-runtime').hexdigest()):
            raise RuntimeError('Actual complete native runtime does not match source profile')
        modules={p.name:hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted((installed/'lib/python3.15/lib-dynload').glob('*.so'))}
        # The full installed CPython tree, including its original test data, is
        # exported unchanged. This build is not full upstream suite acceptance.
        if not modules or not any(n.startswith('_ssl.') for n in modules) or not any(n.startswith('_sqlite3.') for n in modules):
            raise RuntimeError('Full CPython native providers are missing')
        shared.copy_regular(python/'LICENSE',shared.OUTPUT/'CPython-LICENSE')
        cpython={'source':manifest['cpython']['3.15.1'],'modules':modules,
            'python_executable_sha256':hashlib.sha256(executable.read_bytes()).hexdigest(),
            'full_install_preserved':True,'source_or_case_edits':False}
    result={'profile':profile,'architecture':architecture,'os_release':release,'build_uid':os.geteuid(),
        'openssl':tls,'sqlite':database,'cpython':cpython,'runtime':probe,
        'compiler':subprocess.check_output(['cc','--version'],text=True).splitlines()[0],
        'original_Debian_and_Alpine_guards_unchanged':True,'operator_database_opened':False,
        'full_CPython_or_vendor_suite_acceptance':False,'deployment_acceptance':False}
    (shared.OUTPUT/'provenance.json').write_text(json.dumps(result,indent=2)+'\n')
    print(json.dumps(result))


if __name__=='__main__':
    main()
