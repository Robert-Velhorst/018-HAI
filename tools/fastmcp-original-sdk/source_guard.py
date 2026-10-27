"""Bind hosted original SDK cases to every immutable publisher source payload."""
from pathlib import Path, PurePosixPath
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys


def git(*args):
    return subprocess.check_output(['git', *args]).decode().strip()


def payloads():
    root = Path.cwd().resolve()
    result = {}
    raw = subprocess.check_output(['git', 'ls-tree', '-r', '-z', 'HEAD'])
    for entry in raw.split(b'\0'):
        if not entry:
            continue
        meta, name = entry.split(b'\t', 1)
        mode, kind, blob = meta.decode().split()
        name = name.decode('utf-8')
        relative = PurePosixPath(name)
        assert not relative.is_absolute() and '..' not in relative.parts
        assert kind == 'blob' and mode in {'100644', '100755', '120000'}
        path = root.joinpath(*relative.parts)
        if mode == '120000' and path.is_symlink():
            data = os.readlink(path).encode('utf-8')
            representation = 'native-symlink'
        else:
            assert path.is_file() and not path.is_symlink()
            assert path.resolve().is_relative_to(root)
            if mode == '120000':
                assert os.name == 'nt' and git('config', '--get', 'core.symlinks') == 'false'
            data = path.read_bytes()
            representation = 'Windows-Git-symlink-target-file' if mode == '120000' else 'regular-file'
        actual = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
        assert actual == blob, name
        result[name] = {'blob': blob, 'sha256': hashlib.sha256(data).hexdigest(), 'mode': mode, 'representation': representation}
    return result


def main():
    assert os.environ.get('GITHUB_ACTIONS') == 'true'
    assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
    phase = sys.argv[1]
    assert phase in {'initialise', 'adopt', 'verify'}
    reports = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-fastmcp-original-sdk'
    reports.mkdir(exist_ok=True)
    if phase == 'initialise':
        source = Path(__file__).resolve().parent
        for name in ['source_guard.py', 'spec.json']:
            shutil.copyfile(source / name, reports / name)
        record = {'head': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
                  'project_worker_head': os.environ['GITHUB_SHA'], 'all_project_payloads': payloads()}
        (reports / 'hai-harness-source-identity.json').write_text(json.dumps(record, indent=2)+'\n')
        return
    spec = json.loads((reports / 'spec.json').read_text())
    assert git('rev-parse', 'HEAD') == spec['publisher_commit']
    assert git('rev-parse', 'HEAD^{tree}') == spec['publisher_tree']
    assert git('rev-parse', spec['publisher_tag'] + '^{commit}') == spec['publisher_commit']
    assert not git('status', '--porcelain', '--untracked-files=no')
    actual = payloads()
    assert len(actual) == spec['publisher_blob_count']
    if phase == 'adopt':
        (reports / 'publisher-original-payloads.json').write_text(json.dumps(actual, indent=2)+'\n')
    else:
        assert actual == json.loads((reports / 'publisher-original-payloads.json').read_text())
        (reports / 'publisher-source-integrity-after.json').write_text(json.dumps({
            'project_worker_head': os.environ['GITHUB_SHA'], 'publisher_commit': spec['publisher_commit'],
            'all_original_payloads_unchanged': len(actual), 'original_cases_helpers_assertions_fixtures_selectors_unchanged': True,
            'SDK_python': platform.python_version(), 'SDK_platform': platform.system(),
            'SDK_uv': subprocess.check_output(['uv', '--version']).decode().strip(),
            'SDK_results_runtime_images_and_live_acceptance_are_separate': True}, indent=2)+'\n')
    print('All immutable publisher source payloads reconcile; SDK case outcomes remain separate.')


if __name__ == '__main__':
    main()
