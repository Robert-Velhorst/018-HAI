"""Inventory an immutable public base; never claim runner/native acceptance."""
from pathlib import Path
import hashlib
import importlib
import importlib.metadata
import importlib.util
import json
import os
import platform
import ssl
import sqlite3
import sys
import sysconfig


def main():
    if (sys.platform != 'linux' or os.geteuid() != 10001
            or os.environ.get('HAI_IMAGE_BASE_PREFLIGHT') != '1'
            or Path(__file__).resolve() != Path('/review/base_preflight.py')):
        raise RuntimeError('Use only the read-only disposable image preflight')
    if platform.python_version() != '3.15.1':
        raise RuntimeError('The selected base has an unexpected Python version')
    availability = {}
    for name in sorted(sys.stdlib_module_names):
        try:
            availability[name] = importlib.util.find_spec(name) is not None
        except (ImportError, ValueError, AttributeError) as error:
            availability[name] = {'lookup_error': type(error).__name__}
    imports = {}
    for name in ['_ssl', '_hashlib', '_sqlite3', '_socket', '_ctypes', 'zlib',
                 '_bz2', '_lzma', '_decimal', '_uuid', 'readline', '_tkinter',
                 '_curses', '_dbm', '_gdbm']:
        try:
            module = importlib.import_module(name)
            imports[name] = {'imported': True, 'file': getattr(module, '__file__', None)}
        except ImportError as error:
            imports[name] = {'imported': False, 'error': str(error)}
    native = Path(sysconfig.get_config_var('DESTSHARED'))
    files = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(native.glob('*.so')) if p.is_file()}
    context = ssl.create_default_context()
    result = {
        'scope': 'Public factory base only; no application or original vendor suite acceptance',
        'python': platform.python_version(),
        'python_executable_sha256': hashlib.sha256(Path(sys.executable).read_bytes()).hexdigest(),
        'os_release': Path('/etc/os-release').read_text(),
        'platform': sysconfig.get_platform(), 'host_gnu_type': sysconfig.get_config_var('HOST_GNU_TYPE'),
        'openssl': ssl.OPENSSL_VERSION, 'sqlite': sqlite3.sqlite_version,
        'ca_roots': len(context.get_ca_certs()),
        'stdlib_module_availability': availability, 'native_module_imports': imports,
        'complete_lib_dynload_sha256': files,
        'python_distributions': sorted((d.metadata['Name'], d.version)
            for d in importlib.metadata.distributions()),
        'required_native_profile': {'python': '3.15.1', 'openssl': '4.1.0', 'sqlite': '3.54.1'},
        'original_FastMCP_cases_executed': False,
        'native_profile_or_runner_qualified': False,
        'operator_database_or_provider_accessed': False,
    }
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
