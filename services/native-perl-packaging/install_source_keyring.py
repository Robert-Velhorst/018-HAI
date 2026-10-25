"""Install Debian's official tag2upload package in the disposable ABI SDK."""
from pathlib import Path
import hashlib
import json
import os
import platform
import pwd
import re
import subprocess
import sys


def main():
    if (sys.platform != 'linux' or os.getuid() != 0
            or platform.python_version() != '3.15.1'
            or os.environ.get('HAI_GCC_RUNTIME_SDK_BUILD') != 'github-hosted'):
        raise RuntimeError('Only the disposable hosted ABI SDK may install this package')
    spec_path = Path(__file__).with_name('source-keyring.json')
    spec = json.loads(spec_path.read_text())
    if (spec['package'], spec['version'], spec['architecture']) != ('debian-tag2upload-keyring', '1.2', 'all'):
        raise ValueError('Unexpected official source-signing package')
    digest = lambda raw: hashlib.sha256(raw).hexdigest()
    output = Path('/usr/share/doc/hai-source-keyring-setup')
    output.mkdir(exist_ok=False)
    work = Path('/tmp/hai-source-keyring-download')
    work.mkdir(exist_ok=False)
    sandbox = pwd.getpwnam('_apt')
    os.chown(work, sandbox.pw_uid, sandbox.pw_gid)
    # The runtime installer has already configured the authenticated Debian sid
    # repository and negatively pinned every unlisted sid package.
    policy = Path('/etc/apt/preferences.d/hai-source-keyring')
    if policy.exists():
        raise RuntimeError('Do not replace an existing package policy')
    policy.write_text('Package: debian-tag2upload-keyring\nPin: version 1.2\nPin-Priority: 990\n')
    env = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/root',
           'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive'}
    report = {'spec_sha256': digest(spec_path.read_bytes()), 'commands': [],
              'normal_authenticated_install_and_owned_keyring_passed': False,
              'signature_policy_or_operator_trust_store_changed': False}

    def run(name, argv):
        path = output/(name+'.log')
        with path.open('wb') as handle:
            result = subprocess.run(argv, cwd=work, env=env, stdout=handle,
                                    stderr=subprocess.STDOUT, timeout=600)
        report['commands'].append({'name': name, 'argv': argv,
                                   'returncode': result.returncode,
                                   'log_sha256': digest(path.read_bytes())})
        if result.returncode:
            print(path.read_text(errors='replace')[-12000:], file=sys.stderr)
            raise RuntimeError(name+' failed under normal package policy')
        return path.read_text(errors='replace')

    try:
        run('authenticated-download', ['apt-get', 'download', spec['package']+'='+spec['version']])
        archive = work/Path(spec['filename']).name
        raw = archive.read_bytes()
        if len(raw) != spec['bytes'] or digest(raw) != spec['sha256']:
            raise ValueError('Official keyring package differs from reviewed archive')
        control = run('package-control', ['dpkg-deb', '--field', str(archive)])
        for field in ['Package', 'Version', 'Architecture']:
            match = re.search(r'(?m)^'+field+r': (.+)$', control)
            if match is None or match[1] != spec[field.lower()]:
                raise ValueError('Source-keyring package control differs')
        run('no-remove-simulation', ['apt-get', '--simulate', '--no-remove', 'install', str(archive)])
        run('normal-install', ['apt-get', '-y', '--no-remove', 'install', str(archive)])
        status = run('installed-status', ['dpkg-query', '-W', '-f=${Version}\t${db:Status-Status}\n', spec['package']])
        if status.strip() != spec['version']+'\tinstalled':
            raise RuntimeError('Exact source-keyring package is not configured')
        keyring = Path(spec['keyring_path'])
        if digest(keyring.read_bytes()) != spec['keyring_sha256']:
            raise RuntimeError('Official installed public keyring payload differs')
        owner = run('keyring-ownership', ['dpkg-query', '--search', str(keyring)]).strip()
        if not owner.startswith(spec['package']+': '):
            raise RuntimeError('Source keyring lacks its actual package ownership')
        if run('dpkg-audit', ['dpkg', '--audit']).strip():
            raise RuntimeError('Source-keyring package state is incomplete')
        report.update({'normal_authenticated_install_and_owned_keyring_passed': True,
                       'keyring_path': str(keyring), 'keyring_sha256': spec['keyring_sha256'],
                       'package': spec['package'], 'version': spec['version'], 'owner': owner})
    finally:
        (output/'qualification.json').write_text(json.dumps(report, indent=2)+'\n')


if __name__ == '__main__':
    main()
