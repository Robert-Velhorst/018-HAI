"""Package already-compiled root-owned payloads with ordinary APK signatures.

No native compilation occurs here. The one-build private signing key is never
exported; the public key and package fingerprints are retained for inspection.
"""
from pathlib import Path
import hashlib
import json
import os
import subprocess
import sys
import tempfile


def command(args):
    subprocess.run(args, check=True, timeout=120)


def main():
    root = Path('/native-pack')
    if (sys.platform != 'linux' or os.geteuid() != 0
            or Path(__file__).resolve().parent != root
            or os.environ.get('HAI_DISPOSABLE_APK_PACKAGER') != '1'
            or not Path('/etc/alpine-release').read_text().strip().startswith('3.24.')):
        raise RuntimeError('APK metadata packaging requires its separate disposable root-owned stage')
    output = root / 'out'
    output.mkdir()
    keys = output / 'keys'
    keys.mkdir()
    public = keys / 'hai-native-runtime.rsa.pub'
    packages = {}
    with tempfile.TemporaryDirectory(prefix='hai-apk-signing-') as temporary:
        private = Path(temporary) / 'one-build-signing-key.pem'
        command(['openssl', 'genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:3072', '-out', str(private)])
        os.chmod(private, 0o600)
        command(['openssl', 'pkey', '-in', str(private), '-pubout', '-out', str(public)])
        for name, expected in [('hai-openssl4', '4.1.0-r0'), ('hai-sqlite3', '3.54.1-r0')]:
            spec = json.loads((root / 'input' / (name + '.json')).read_text())
            payload = root / 'input' / spec['payload_root']
            if (spec['name'] != name or spec['version'] != expected
                    or spec['architecture'] != 'x86_64' or spec['compilation_uid'] != 10001
                    or payload.resolve().parent != root / 'input/payloads'):
                raise RuntimeError('Unrecognized native package payload')
            actual = {}
            for file in payload.rglob('*'):
                if file.is_symlink() or file.stat().st_uid != 0 or file.stat().st_gid != 0:
                    raise RuntimeError('Package payload must be root owned and contain no symlinks')
                if file.is_file():
                    actual[file.relative_to(payload).as_posix()] = hashlib.sha256(file.read_bytes()).hexdigest()
            if actual != spec['files']:
                raise RuntimeError('Native payload changed after the non-root source build')
            archive = output / (name + '.apk')
            infos = {'name': name, 'version': expected, 'arch': 'x86_64', 'origin': spec['origin'],
                     'description': 'HAI pinned source-built native client runtime',
                     'license': 'Apache-2.0' if name == 'hai-openssl4' else 'blessing',
                     'depends': 'musl',
                     'provides': 'so:libssl.so.4=4.1.0 so:libcrypto.so.4=4.1.0' if name == 'hai-openssl4' else 'so:libsqlite3.so.0=3.54.1'}
            args = ['apk', 'mkpkg', '--files', str(payload), '--output', str(archive), '--sign-key', str(private)]
            for key, value in infos.items():
                args.extend(['--info', key + ':' + value])
            command(args)
            command(['apk', '--keys-dir', str(keys), 'verify', str(archive)])
            empty_keys = Path(temporary) / 'empty-trust'
            empty_keys.mkdir(exist_ok=True)
            rejected = subprocess.run(['apk', '--keys-dir', str(empty_keys), 'verify', str(archive)],
                capture_output=True, timeout=120)
            if rejected.returncode == 0:
                raise RuntimeError('APK accepted a signature without its public trust key')
            (output / (name + '-untrusted-rejection.log')).write_bytes(rejected.stdout + rejected.stderr)
            packages[name] = {'version': expected, 'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
                              'source_payload_files': spec['files'], 'signature_verified': True,
                              'untrusted_signature_rejected': True}
    result = {'packages': packages, 'public_key_sha256': hashlib.sha256(public.read_bytes()).hexdigest(),
              'private_key_exported': False, 'signature_checks_bypassed': False,
              'native_compilation_uid': 10001, 'metadata_packaging_uid': os.geteuid()}
    (output / 'signing-provenance.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
