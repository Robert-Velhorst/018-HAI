"""Hosted-only full Trivy dependency resolution and unchanged upstream suite."""
from pathlib import Path, PurePosixPath
from urllib.request import urlopen
import hashlib
import io
import json
import os
import shutil
import subprocess
import tarfile

assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
assert os.environ.get('GITHUB_REPOSITORY') == 'Robert-Velhorst/018-HAI'
assert os.name == 'posix' and os.getuid() != 0
assert subprocess.check_output(['go', 'version'], text=True).split()[2] == 'go1.27.3'
temp = Path(os.environ['RUNNER_TEMP']).resolve()
out = temp / 'trivy-full-graph-evidence'
out.mkdir(exist_ok=True)
work = temp / 'trivy-full-graph-source'
assert not work.exists()
assert shutil.disk_usage(temp).free >= 12 * 1024**3, 'Retain work; insufficient disposable worker disk for full graph.'
manifest_raw = Path('services/trivy-runner/upstream-stable-targets.json').read_bytes()
manifest = json.loads(manifest_raw)
assert len(manifest['modules']) == 470
(out / 'target-manifest.json').write_bytes(manifest_raw)
hai = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
       'tree': subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], text=True).strip(),
       'tracked_file_sha256': {p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
          for p in subprocess.check_output(['git', 'ls-files', '-z']).decode().split('\0') if p}}
(out / 'HAI-source-identity.json').write_text(json.dumps(hai, indent=2))
raw = urlopen('https://api.github.com/repos/aquasecurity/trivy/tarball/v0.76.1', timeout=120).read()
assert hashlib.sha256(raw).hexdigest() == manifest['archive_sha256']
archive = tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz')
work.mkdir()
links = []
seen = set()
for member in archive.getmembers():
    parts = PurePosixPath(member.name).parts[1:]
    if not parts:
        continue
    assert not member.name.startswith('/') and '..' not in parts
    relative = Path(*parts)
    assert str(relative) not in seen
    seen.add(str(relative))
    dest = work / relative
    assert dest.resolve().is_relative_to(work)
    if member.isdir():
        dest.mkdir(parents=True, exist_ok=True)
    elif member.isfile():
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(archive.extractfile(member).read())
        dest.chmod(member.mode & 0o777)
    elif member.issym():
        assert not Path(member.linkname).is_absolute()
        assert (dest.parent / member.linkname).resolve().is_relative_to(work)
        links.append((dest, member.linkname))
    else:
        raise ValueError('Unexpected upstream archive member type: ' + member.name)
for dest, target in links:
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.symlink_to(target)
assert len(links) == 3, 'Preserve all three original symlink fixtures.'
def inventory():
    result = {}
    for p in work.rglob('*'):
        name = p.relative_to(work).as_posix()
        if p.is_symlink():
            result[name] = {'kind': 'symlink', 'target': os.readlink(p)}
        elif p.is_file():
            result[name] = {'kind': 'file', 'sha256': hashlib.sha256(p.read_bytes()).hexdigest(), 'mode': p.stat().st_mode & 0o777}
    return result
before = inventory()
assert before['go.mod']['sha256'] == manifest['original_go_mod_sha256']
(out / 'original-upstream-source.json').write_text(json.dumps(before, indent=2))
env = os.environ.copy()
env.update(GOTOOLCHAIN='local', GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org', GOFLAGS='', CGO_ENABLED='0')
def command(name, args):
    with (out / (name + '.stdout')).open('wb') as stdout, (out / (name + '.stderr')).open('wb') as stderr:
        p = subprocess.run(args, cwd=work, env=env, stdout=stdout, stderr=stderr)
    return {'args': args, 'returncode': p.returncode}
targets = [r['module'] + '@' + r['stable_target'] for r in manifest['modules'] if r['stable_target'] is not None]
missing = [r['module'] for r in manifest['modules'] if r['stable_target'] is None]
assert len(targets) == 404 and len(missing) == 66
summary = {'source_head': hai['head'], 'normal_resolution_only': True, 'forced_replace_or_toolchain_override_added': False,
           'stable_targets': len(targets), 'paths_without_stable_tags': missing, 'commands': [],
           'all_components_or_runtime_or_deployment_qualified': False}
try:
    result = command('normal-go-get', ['go', 'get', *targets])
    summary['commands'].append(result)
    assert result['returncode'] == 0, 'Normal highest-stable resolution failed; inspect complete log without overriding constraints.'
    result = command('module-integrity', ['go', 'mod', 'verify'])
    summary['commands'].append(result)
    assert result['returncode'] == 0
    result = command('resolved-modules', ['go', 'list', '-m', '-json', 'all'])
    summary['commands'].append(result)
    assert result['returncode'] == 0
    result = command('full-original-upstream-suite', ['go', 'test', '-json', './...'])
    summary['commands'].append(result)
    assert result['returncode'] == 0, 'Full original upstream suite failed; no cases or assertions may be weakened.'
    result = command('complete-original-cli-build', ['go', 'build', '-trimpath', '-ldflags=-s -w -extldflags=-static -X github.com/aquasecurity/trivy/pkg/version/app.ver=0.76.1+hai.dependencies.1', '-o', str(out / 'trivy-candidate'), './cmd/trivy/'])
    summary['commands'].append(result)
    assert result['returncode'] == 0
    assert not missing, 'Published stable tags are still unavailable for declared paths; do not claim complete stable acceptance.'
finally:
    after = inventory()
    changed = sorted(p for p in set(before) | set(after) if before.get(p) != after.get(p))
    summary['upstream_changed_paths'] = changed
    summary['all_original_cases_helpers_fixtures_source_unchanged'] = all(p in {'go.mod', 'go.sum'} for p in changed)
    (out / 'candidate-upstream-source.json').write_text(json.dumps(after, indent=2))
    for name in ['go.mod', 'go.sum']:
        (out / ('candidate-' + name)).write_bytes((work / name).read_bytes())
    (out / 'candidate-progress.json').write_text(json.dumps(summary, indent=2))
    assert summary['all_original_cases_helpers_fixtures_source_unchanged'], 'Unexpected source change must be reviewed.'
