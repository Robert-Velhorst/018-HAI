"""Inspect the complete immutable FastMCP SDK lock before any SDK installer."""
from pathlib import Path
import hashlib
import json
import os
import re
import subprocess
import sys
import tomllib


def stable(version):
    # PEP 440 stable releases/post-releases; local provenance is not a prerelease.
    return isinstance(version, str) and re.fullmatch(
        r"v?(?:[0-9]+!)?[0-9]+(?:\.[0-9]+)*(?:(?:[._-]?post[._-]?[0-9]+)|(?:-[0-9]+))?(?:\+[a-z0-9]+(?:[._-][a-z0-9]+)*)?",
        version, re.IGNORECASE,
    ) is not None


def self_test():
    good = ['1', '1.2.3', '2!1.2.3', '1.2.3.post1', '1.2.3-1', 'v1.2.3', '1.2.3+local', '1.2.3.post1+linux.x86']
    bad = ['0.65b0', '0.23.0rc2', '1.0a1', '1.0.dev1', '1.0.post1.dev2', '1.0-beta2', '1.0pre1', '1.0-preview1', '1.0rc1+stable', 'not-version', '1.2.*', None]
    for value in good:
        assert stable(value), value
    for value in bad:
        assert not stable(value), value
    print(json.dumps({'owned_policy_boundary_cases': len(good) + len(bad), 'passed': True, 'SDK_cases_executed': False}))


def main():
    assert os.environ.get('GITHUB_ACTIONS') == 'true'
    assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
    assert os.environ.get('GITHUB_REPOSITORY') == 'Robert-Velhorst/018-HAI'
    assert os.environ.get('GITHUB_RUN_ID', '').isdigit()
    assert os.environ.get('GITHUB_RUN_ATTEMPT', '').isdigit()
    if sys.argv[1:] == ['--self-test']:
        self_test()
        return
    assert not sys.argv[1:]
    reports = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-fastmcp-original-sdk'
    spec = json.loads((reports / 'spec.json').read_bytes())
    before = json.loads((reports / 'publisher-original-payloads.json').read_bytes())
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip() == spec['publisher_commit']
    raw = Path('uv.lock').read_bytes()
    assert hashlib.sha256(raw).hexdigest() == before['uv.lock']['sha256']
    assert hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest() == before['uv.lock']['blob']
    packages = tomllib.loads(raw.decode())['package']
    assert packages and all(isinstance(p.get('name'), str) and isinstance(p.get('version'), str) for p in packages)
    unapproved = [{'name': p['name'], 'version': p['version'], 'resolution_markers': p.get('resolution-markers')}
                  for p in packages if not stable(p['version'])]
    project = tomllib.loads(Path('pyproject.toml').read_text(encoding='utf-8'))
    default_groups = project.get('tool', {}).get('uv', {}).get('default-groups', ['dev'])
    default_requests = {name: project.get('dependency-groups', {}).get(name, []) for name in default_groups}
    sdk = [p for p in packages if p['name'] == 'opentelemetry-sdk']
    evidence = {'schema_version': 1, 'publisher_commit': spec['publisher_commit'],
        'HAI_worker_head': os.environ['GITHUB_SHA'], 'uv_lock_blob': before['uv.lock']['blob'],
        'uv_lock_sha256': hashlib.sha256(raw).hexdigest(), 'lock_package_records': len(packages),
        'scope': 'Complete original SDK lock union across its declared Linux/Windows compatibility matrix; not a per-interpreter installation inventory.',
        'default_groups': default_groups, 'default_group_requests': default_requests,
        'locked_otel_sdk_dependencies': [{'version': p['version'], 'dependencies': p.get('dependencies', [])} for p in sdk],
        'unapproved_nonstable_rows': unapproved, 'approved_prerelease_exceptions_for_this_FastMCP_SDK': {},
        'DeepEval_exception_does_not_apply': True, 'passed': not unapproved,
        'package_manager_SDK_or_case_execution': False, 'complete_original_SDK_qualified': False}
    (reports / 'locked-sdk-stability-policy.json').write_text(json.dumps(evidence, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({'lock_package_records': len(packages), 'unapproved_nonstable_rows': unapproved,
                      'passed': not unapproved, 'SDK_installer_started': False}))
    if unapproved:
        raise SystemExit('Unapproved prerelease SDK lock: original installers and cases must not start')


if __name__ == '__main__':
    main()
