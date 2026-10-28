"""Check declared Python/npm lock stability before general HAI CI installers.

Metadata only. This is not a latest-version, vulnerability, resolved-platform,
SDK, runtime, image, or deployment acceptance check. No exceptions apply here.
"""
from pathlib import Path, PurePosixPath
import hashlib
import importlib.util
import json
import os
import re
import subprocess
import sys

sys.dont_write_bytecode = True


def load_owned(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def selected(name):
    parts = name.split('/')
    return (len(parts) == 3 and parts[0] == 'services' and parts[2].startswith('requirements')
            and parts[2].endswith(('.lock', '.txt'))) or (
        name.endswith('/package-lock.json') and name.startswith(('frontend/', 'services/', 'tools/browser-acceptance/')))


def npm_stable(version):
    return isinstance(version, str) and re.fullmatch(
        r'(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?', version) is not None


def python_rows(raw, stable):
    rows, errors = [], []
    for line_number, line in enumerate(raw.decode('utf-8').splitlines(), 1):
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        if line == '--index-url https://pypi.org/simple':
            continue
        if re.fullmatch(r'--hash=sha256:[0-9a-f]{64}(?:\s+\\)?', line):
            continue
        match = re.fullmatch(r'([A-Za-z0-9][A-Za-z0-9._-]*)(?:\[[A-Za-z0-9,._-]+\])?==([^\s;\\]+)(?:\s*;\s*[^\\]+)?(?:\s+\\)?', line)
        if not match:
            errors.append({'line': line_number, 'reason': 'unrecognized-or-unpinned-lock-syntax'})
            continue
        name, version = match.groups()
        rows.append({'name': name.lower().replace('_', '-').replace('.', '-'), 'version': version,
                     'line': line_number, 'stable': stable(version)})
    if not rows:
        errors.append({'reason': 'no-versioned-rows'})
    return rows, errors


def npm_rows(raw, repo, source):
    data = json.loads(raw)
    rows, errors, workspaces = [], [], []
    assert data.get('lockfileVersion') == 3 and isinstance(data.get('packages'), dict)
    for name, package in data['packages'].items():
        assert isinstance(package, dict)
        if package.get('link') is True:
            target = package.get('resolved')
            if not isinstance(target, str) or not (source.parent / target).resolve().is_relative_to(repo):
                errors.append({'entry': name, 'reason': 'unbound-workspace-link'})
            else:
                workspaces.append({'entry': name, 'resolved_within_repository': True})
            continue
        version = package.get('version')
        if name == '' and version is None:
            workspaces.append({'entry': '', 'reason': 'source-bound-root-project'})
            continue
        if not isinstance(version, str):
            errors.append({'entry': name, 'reason': 'missing-registry-version'})
            continue
        rows.append({'name': name, 'version': version, 'stable': npm_stable(version)})
    if not rows:
        errors.append({'reason': 'no-versioned-rows'})
    return rows, errors, workspaces


def boundary_tests(stable):
    count = 0
    for value, expected in [('1.2.3', True), ('0.0.0', True), ('1.2.3+build.12', True),
                            ('1.2.3-beta.1', False), ('1.2.3-rc.0+release', False),
                            ('01.2.3', False), ('1.2', False), ('file:../local', False), (None, False)]:
        assert npm_stable(value) == expected
        count += 1
    raw = b'# header\n--index-url https://pypi.org/simple\nx[extra]==1.2.3 ; python_version >= "3.13" \\\n    --hash=sha256:' + b'a' * 64 + b'\ny==0.66b1\nz==1.0rc1\na==1.0.post1\n'
    rows, errors = python_rows(raw, stable)
    assert not errors and [r['stable'] for r in rows] == [True, False, False, True]
    count += 1
    for raw in [b'-r other.lock', b'name>=1', b'name @ https://example.invalid/file.whl', b'--extra-index-url https://example.invalid', b'--hash=md5:abc']:
        rows, errors = python_rows(raw, stable)
        assert errors
        count += 1
    root = Path.cwd().resolve()
    rows, errors, workspaces = npm_rows(json.dumps({'lockfileVersion': 3, 'packages': {
        '': {}, 'node_modules/a': {'version': '1.2.3'},
        'node_modules/b': {'version': '2.0.0-beta.1'},
        'node_modules/c': {'link': True, 'resolved': '../outside'},
        'node_modules/d': {}}}).encode(), root, root / 'package-lock.json')
    assert len(rows) == 2 and [r['stable'] for r in rows] == [True, False]
    assert len(errors) == 2 and len(workspaces) == 1
    return count + 1


def main():
    assert not sys.argv[1:]
    assert os.environ.get('GITHUB_ACTIONS') == 'true'
    assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
    assert os.environ.get('GITHUB_REPOSITORY') == 'Robert-Velhorst/018-HAI'
    root = Path.cwd().resolve()
    assert Path(os.environ['GITHUB_WORKSPACE']).resolve() == root
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip() == os.environ['GITHUB_SHA']
    evidence = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-ci-lock-policy'
    assert not evidence.is_relative_to(root)
    evidence.mkdir(exist_ok=False)
    owned = root / 'tools/fastmcp-original-sdk'
    policy = load_owned('hai_owned_stability', owned / 'stable_lock_policy.py')
    metadata = load_owned('hai_owned_metadata', owned / 'source_guard.py')
    before = metadata.payloads()
    (evidence / 'source-before.json').write_text(json.dumps(before, indent=2) + '\n')
    policy.self_test()
    cases = boundary_tests(policy.stable)
    (evidence / 'owned-boundary-cases.json').write_text(json.dumps({'additional_cases': cases, 'passed': True, 'SDK_cases_executed': False}) + '\n')
    inputs = sorted(name for name in before if selected(name))
    assert inputs
    results = []
    for name in inputs:
        source = root.joinpath(*PurePosixPath(name).parts)
        assert source.resolve(strict=True).is_relative_to(root)
        raw = source.read_bytes()
        if name.endswith('/package-lock.json'):
            rows, errors, workspaces = npm_rows(raw, root, source)
        else:
            rows, errors = python_rows(raw, policy.stable)
            workspaces = []
        results.append({'path': name, 'canonical_Git_record': before[name],
                        'consumed_bytes_sha256': hashlib.sha256(raw).hexdigest(),
                        'versioned_rows': len(rows), 'unapproved_nonstable_rows': [r for r in rows if not r['stable']],
                        'syntax_errors': errors, 'source_bound_workspace_records': workspaces})
    after = metadata.payloads()
    assert after == before
    (evidence / 'source-after.json').write_text(json.dumps(after, indent=2) + '\n')
    passed = all(not row['unapproved_nonstable_rows'] and not row['syntax_errors'] for row in results)
    report = {'head': os.environ['GITHUB_SHA'], 'tree': subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}']).decode().strip(),
              'scope': 'Declared Python/npm lock union across general CI; not a resolved platform installation or complete SDK/runtime acceptance.',
              'input_count': len(inputs), 'all_source_payloads_unchanged': len(after), 'inputs': results,
              'approved_prerelease_exceptions': {}, 'DeepEval_isolated_exception_does_not_apply': True,
              'package_manager_SDK_or_case_execution': False, 'passed': passed}
    (evidence / 'lock-stability-policy.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'input_count': len(inputs), 'versioned_rows': sum(r['versioned_rows'] for r in results),
                      'nonstable_rows': sum(len(r['unapproved_nonstable_rows']) for r in results),
                      'syntax_errors': sum(len(r['syntax_errors']) for r in results), 'passed': passed}))
    if not passed:
        raise SystemExit('General CI lock policy rejected inputs; dependent installers must not start')


if __name__ == '__main__':
    main()
