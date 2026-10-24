"""Refresh one existing certificate only in the disposable package SDK image."""
from pathlib import Path
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time


def main():
    if (sys.platform != 'linux' or os.getuid() != 0
            or os.environ.get('HAI_HOSTED_KEYRING_SDK') != 'github-hosted'):
        raise RuntimeError('Use only the explicit hosted SDK image preparation')
    folder = Path(__file__).resolve().parent
    spec = json.loads((folder/'sources.json').read_text(encoding='utf-8'))
    expected = '76A28E42C9811D91E88FBA5E2EC0FFB3B7301B1F'
    if spec['existing_official_primary_fingerprint'] != expected:
        raise ValueError('Original official signer identity changed')
    certificate = folder/'perl-maintainer-renewal.asc'
    item = next(x for x in spec['inputs'] if x['name'] == certificate.name)
    sha = lambda raw: hashlib.sha256(raw).hexdigest()
    raw = certificate.read_bytes()
    if len(raw) != item['bytes'] or sha(raw) != item['sha256']:
        raise ValueError('Pinned official public renewal changed')
    official = Path('/usr/share/keyrings/debian-keyring.gpg')
    baseline = official.with_name('hai-perl-original-debian-keyring.gpg')
    shutil.copyfile(official, baseline)

    with tempfile.TemporaryDirectory(prefix='hai-sdk-public-keyring-') as temporary:
        home = Path(temporary)
        keyring = home/'official.gpg'
        shutil.copyfile(official, keyring)
        gpg = ['gpg', '--batch', '--no-options', '--homedir', str(home),
               '--no-default-keyring', '--keyring', str(keyring)]

        def command(arguments, timeout=120):
            print('SDK public keyring operation:', ' '.join(arguments), flush=True)
            started = time.monotonic()
            try:
                result = subprocess.run(gpg+arguments, capture_output=True, timeout=timeout)
            except subprocess.TimeoutExpired as error:
                # Retain public-tool diagnostics even if SDK construction stops
                # before a runnable image or normal output mount exists.
                for label, value in [('stdout', error.stdout), ('stderr', error.stderr)]:
                    print(label+':', (value or b'')[-65536:].decode('utf-8', errors='replace'), flush=True)
                raise
            print('SDK public keyring operation seconds:', round(time.monotonic()-started, 2), flush=True)
            if result.returncode:
                raise RuntimeError('Normal public certificate operation failed: '
                                   +result.stderr.decode('utf-8', errors='replace'))
            return result.stdout

        def primaries(raw):
            values = set()
            waiting = False
            for line in raw.decode().splitlines():
                if line.startswith(('sec:', 'ssb:')):
                    raise ValueError('A secret certificate was supplied')
                if line.startswith('pub:'):
                    waiting = True
                elif waiting and line.startswith('fpr:'):
                    values.add(line.split(':')[9])
                    waiting = False
            return values

        before = primaries(command(['--with-colons', '--list-keys']))
        renewal = command(['--with-colons', '--show-keys', str(certificate)])
        if expected not in before or primaries(renewal) != {expected}:
            raise ValueError('Renewal is not the exact existing official primary key')
        # Merge into the full existing bundle. Normal import preserves existing
        # revocations and never changes ownertrust or signature/time policy.
        # This updates a complete official bundle, rather than the isolated
        # single-certificate store used by the preceding hosted experiment.
        command(['--import', str(certificate)], timeout=600)
        after = primaries(command(['--with-colons', '--list-keys']))
        if before != after:
            raise ValueError('SDK keyring primary identities changed')
        active = command(['--with-colons', '--list-keys', expected]).decode()
        primary = next(line.split(':') for line in active.splitlines() if line.startswith('pub:'))
        if primary[1] in {'e', 'r', 'd'} or (primary[6] and int(primary[6]) <= time.time()):
            raise RuntimeError('Normal merged certificate is expired, revoked or disabled')
        updated = command(['--export'])
        if not updated or len(updated) > 64*1024*1024:
            raise ValueError('Unexpected official public keyring size')
        pending = official.with_name('.hai-perl-keyring-pending.gpg')
        pending.write_bytes(updated)
        pending.chmod(0o644)
        os.replace(pending, official)
        proof = {
            'original_official_keyring_sha256': sha(baseline.read_bytes()),
            'updated_official_keyring_sha256': sha(updated),
            'existing_primary_fingerprint': expected,
            'public_renewal_input_sha256': item['sha256'],
            'primary_key_count_preserved': len(before),
            'primary_identity_set_sha256': sha('\n'.join(sorted(before)).encode()),
            'renewed_primary_expiry_unix': primary[6],
            'original_and_renewed_primary_identity_sets_equal': True,
            'default_dpkg_signature_backend_changed': False,
            'signature_algorithm_time_revocation_or_ownertrust_policy_changed': False,
            'operator_or_server_trust_store_changed': False,
        }
        (folder/'renewal-proof.json').write_text(json.dumps(proof, indent=2)+'\n', encoding='utf-8')


if __name__ == '__main__':
    main()
