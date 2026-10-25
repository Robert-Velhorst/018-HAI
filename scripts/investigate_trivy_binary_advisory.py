"""Investigate a module-level advisory on the exact published binary, hosted only."""
from pathlib import Path
from urllib.request import urlopen
import hashlib
import io
import json
import os
import subprocess
import tarfile

assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
assert os.environ.get('GITHUB_REPOSITORY') == 'Robert-Velhorst/018-HAI'
assert os.name == 'posix' and os.getuid() != 0
out = Path(os.environ['RUNNER_TEMP']) / 'trivy-openpgp-evidence'
out.mkdir(exist_ok=True)
def record(name, args):
    p = subprocess.run(args, capture_output=True)
    (out / (name + '.stdout')).write_bytes(p.stdout)
    (out / (name + '.stderr')).write_bytes(p.stderr)
    return {'args': args, 'returncode': p.returncode,
            'stdout_sha256': hashlib.sha256(p.stdout).hexdigest(),
            'stderr_sha256': hashlib.sha256(p.stderr).hexdigest()}
source = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
          'tree': subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], text=True).strip()}
source['tracked_file_sha256'] = {p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
    for p in subprocess.check_output(['git', 'ls-files', '-z']).decode().split('\0') if p}
(out / 'source-identity.json').write_text(json.dumps(source, indent=2))
assert subprocess.check_output(['go', 'version'], text=True).split()[2] == 'go1.27.3'
asset_url = 'https://github.com/aquasecurity/trivy/releases/download/v0.76.1/trivy_0.76.1_Linux-64bit.tar.gz'
raw = urlopen(asset_url, timeout=120).read()
assert hashlib.sha256(raw).hexdigest() == 'c7cbe3137fb8a48a17574bede3dd6beb16742d1e9772929940fc90082c39d9c0'
archive = tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz')
members = [m for m in archive.getmembers() if m.isfile() and m.name == 'trivy']
assert len(members) == 1
binary = archive.extractfile(members[0]).read()
(out / 'trivy-published-binary').write_bytes(binary)
image = 'aquasec/trivy:0.76.1@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c'
subprocess.run(['docker', 'pull', '--platform', 'linux/amd64', image], check=True)
cid = subprocess.check_output(['docker', 'create', '--platform', 'linux/amd64', image], text=True).strip()
subprocess.run(['docker', 'cp', cid + ':/usr/local/bin/trivy', str(out / 'trivy-image-binary')], check=True)
subprocess.run(['docker', 'rm', cid], check=True)
image_binary = (out / 'trivy-image-binary').read_bytes()
assert binary == image_binary, 'Published archive is not identical to the image binary; do not promote its result.'
identity = {'image': image, 'release_archive_sha256': hashlib.sha256(raw).hexdigest(),
            'binary_sha256': hashlib.sha256(binary).hexdigest(), 'binary_bytes': len(binary),
            'published_and_image_binary_identical': True}
(out / 'binary-identity.json').write_text(json.dumps(identity, indent=2))
info = record('binary-buildinfo', ['go', 'version', '-m', str(out / 'trivy-published-binary')])
assert info['returncode'] == 0
install = record('govulncheck-install', ['go', 'install', 'golang.org/x/vuln/cmd/govulncheck@v1.1.4'])
assert install['returncode'] == 0
gopath = subprocess.check_output(['go', 'env', 'GOPATH'], text=True).strip()
tool = str(Path(gopath) / 'bin/govulncheck')
tool_info = record('govulncheck-buildinfo', ['go', 'version', '-m', tool])
assert tool_info['returncode'] == 0
scan = record('govulncheck-binary', [tool, '-mode=binary', '-json', str(out / 'trivy-published-binary')])
summary = {'source_head': source['head'], 'binary': identity, 'tool_install': install,
           'binary_buildinfo': info, 'tool_buildinfo': tool_info, 'scan': scan,
           'raw_Trivy_gate_unchanged_and_still_required': True,
           'application_or_original_cases_modified': False,
           'package_level_investigation_not_an_advisory_waiver': True,
           'full_SDK_vendor_runtime_or_deployment_acceptance': False}
(out / 'investigation.json').write_text(json.dumps(summary, indent=2))
assert scan['returncode'] == 0, 'Binary-aware vulnerability investigation reports findings or failed; inspect retained evidence.'
