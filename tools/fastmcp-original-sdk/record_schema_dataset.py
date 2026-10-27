"""Record the original public schema corpus and unchanged publisher results."""
from pathlib import Path, PurePosixPath
import ast
import hashlib
import json
import os
import shutil
import subprocess
import sys


def git(*args):
    return subprocess.check_output(['git', '-C', '/tmp/openapi-directory', *args]).decode().strip()


def main():
    assert os.environ.get('GITHUB_ACTIONS') == 'true'
    assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
    assert os.environ.get('RUNNER_OS') == 'Linux'
    reports = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-fastmcp-original-sdk'
    spec = json.loads((reports / 'spec.json').read_text())['schema_dataset']
    phase = sys.argv[1]
    assert phase in {'before', 'after'}
    identity = {'head': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
                'origin': git('remote', 'get-url', 'origin')}
    if phase == 'before':
        identity['scope'] = 'Initial original-workflow clone; original fixture selects its pinned commit during collection.'
        (reports / 'schema-initial-clone-identity.json').write_text(json.dumps(identity, indent=2)+'\n')
        return
    assert identity['head'] == spec['commit'] and identity['tree'] == spec['tree']
    assert not git('status', '--porcelain', '--untracked-files=no')
    root = Path('/tmp/openapi-directory').resolve(strict=True)
    records = {}
    raw = subprocess.check_output(['git', '-C', str(root), 'ls-tree', '-r', '-z', 'HEAD'])
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
        if mode == '120000':
            assert path.is_symlink()
            data = os.readlink(path).encode('utf-8')
        else:
            assert path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(root)
            data = path.read_bytes()
        actual = hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
        assert actual == blob, name
        records[name] = {'blob': blob, 'sha256': hashlib.sha256(data).hexdigest(), 'mode': mode}
    assert len(records) == spec['blob_count']
    (reports / 'schema-complete-dataset-payloads.json').write_text(json.dumps(records, indent=2)+'\n')
    result_dir = Path('/tmp/schema_crash_results')
    files = sorted(result_dir.glob('*.json'))
    metrics = ('schemas', 'type_errors', 'schema_errors', 'timeouts', 'other_errors')
    totals = dict.fromkeys(metrics, 0)
    retained = reports / 'schema-provider-results'
    retained.mkdir(exist_ok=True)
    for path in files:
        assert path.is_file() and not path.is_symlink()
        row = json.loads(path.read_text())
        for metric in metrics:
            assert type(row[metric]) is int and row[metric] >= 0
            totals[metric] += row[metric]
        shutil.copyfile(path, retained / path.name)
    failures = Path('/tmp/schema_failures')
    if failures.exists():
        retained_failures = reports / 'schema-failure-details'
        retained_failures.mkdir(exist_ok=True)
        for path in sorted(failures.glob('*.jsonl')):
            assert path.is_file() and not path.is_symlink()
            shutil.copyfile(path, retained_failures / path.name)
    source = Path('tests/utilities/json_schema_type/conftest.py').read_bytes()
    caps = {}
    for node in ast.walk(ast.parse(source)):
        if isinstance(node, ast.Assign) and isinstance(node.value, ast.Constant):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id.startswith('MAX_'):
                    caps[target.id] = node.value.value
    assert caps == {'MAX_TYPE_ERRORS': 0, 'MAX_SCHEMA_ERRORS': 5, 'MAX_TIMEOUTS': 5, 'MAX_OTHER_ERRORS': 50}
    status = int((reports / 'schema-case.exit-status').read_text().strip())
    result = {'dataset': identity, 'all_original_dataset_payloads_verified': len(records),
              'provider_result_files': len(files), 'raw_totals': totals,
              'original_case_exit_status': status, 'unchanged_publisher_baseline_caps': caps,
              'original_conftest_sha256': hashlib.sha256(source).hexdigest(),
              'case_pass_does_not_mean_zero_raw_errors': True,
              'baseline_caps_timeouts_or_original_case_selectors_modified': False,
              'complete_original_SDK_or_live_data_qualified': False}
    (reports / 'schema-dataset-and-raw-results.json').write_text(json.dumps(result, indent=2)+'\n')
    assert files and totals['schemas'] > 0, 'No original schema corpus results; refusing empty acceptance.'
    print(json.dumps({'dataset_payloads_verified': len(records), 'provider_results': len(files),
                      'raw_totals': totals, 'original_case_exit_status': status}))


if __name__ == '__main__':
    main()
