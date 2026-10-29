"""Verify publisher release provenance without installing or executing its SDK."""
from pathlib import Path
import hashlib
import importlib.util
import json
import os
import subprocess
import sys

sys.dont_write_bytecode = True
assert not sys.argv[1:]
assert os.environ.get('GITHUB_ACTIONS') == 'true'
assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
assert os.environ.get('GITHUB_REPOSITORY') == 'Robert-Velhorst/018-HAI'
project = Path.cwd().resolve()
reports = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-node-release-provenance'
assert not reports.is_relative_to(project)
reports.mkdir(exist_ok=False)
spec = json.loads(Path('tools/node-release-provenance/spec.json').read_bytes())
loader = importlib.util.spec_from_file_location('hai_owned_source_metadata', project / 'tools/fastmcp-original-sdk/source_guard.py')
metadata = importlib.util.module_from_spec(loader)
loader.loader.exec_module(metadata)
project_before = metadata.payloads()
(reports / 'HAI-source-before.json').write_text(json.dumps(project_before, indent=2) + '\n')
for name, expected in [('release-signer.asc', spec['existing_signer_key_sha256']),
                       ('active-release-keys.kbx', spec['existing_active_keyring_sha256'])]:
    raw = (project / 'tools/node-native-sdk' / name).read_bytes()
    assert hashlib.sha256(raw).hexdigest() == expected
public_home = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-node-release-public-keyring'
public_home.mkdir(mode=0o700)
env = {**os.environ, 'GNUPGHOME': str(public_home)}
source = Path(os.environ['RUNNER_TEMP']).resolve() / 'hai-node-26130-source'
source.mkdir(exist_ok=False)


def command(name, args, cwd=source):
    result = subprocess.run(args, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (reports / (name + '.log')).write_bytes(result.stdout)
    assert result.returncode == 0, name
    return result.stdout.decode(errors='replace')


approved_bundle = public_home / 'approved-active-keyring.kbx'
approved_bundle.write_bytes((project / 'tools/node-native-sdk/active-release-keys.kbx').read_bytes())
membership = command('verify-current-key-is-in-existing-approved-active-bundle', [
    'gpg', '--batch', '--no-default-keyring', '--keyring', str(approved_bundle),
    '--with-colons', '--fingerprint', '--list-keys', spec['expected_current_signer_fingerprint']])
fingerprints = [line.split(':')[9] for line in membership.splitlines() if line.startswith('fpr:')]
assert spec['expected_current_signer_fingerprint'] in fingerprints
current_key = project / 'tools/node-release-provenance/release-signer-current.asc'
assert hashlib.sha256(current_key.read_bytes()).hexdigest() == spec['current_signer_public_key_sha256']
command('import-matching-official-key-from-existing-trust-set', ['gpg', '--batch', '--import', str(current_key)])
command('init', ['git', 'init', '-q'])
command('origin', ['git', 'remote', 'add', 'origin', 'https://github.com/nodejs/node.git'])
command('fetch-signed-release', ['git', 'fetch', '--depth=1', 'origin', 'refs/tags/' + spec['tag'] + ':refs/tags/' + spec['tag']])
signature = command('verify-signed-release', ['git', 'verify-tag', '--raw', spec['tag']])
valid = [line.split() for line in signature.splitlines() if line.startswith('[GNUPG:] VALIDSIG ')]
assert any(spec['expected_current_signer_fingerprint'] in (line[2], line[-1]) for line in valid)
command('checkout-exact-publisher-commit', ['git', 'checkout', '--detach', spec['commit']])
git = lambda *args: subprocess.check_output(['git', *args], cwd=source, env=env).decode().strip()
assert git('rev-parse', 'HEAD') == spec['commit']
assert git('rev-parse', 'HEAD^{tree}') == spec['tree']
assert git('rev-parse', spec['tag']) == spec['tag_object']
payloads = {}
for row in subprocess.check_output(['git', 'ls-tree', '-r', '-z', 'HEAD'], cwd=source, env=env).split(b'\0'):
    if not row:
        continue
    meta, encoded = row.split(b'\t', 1)
    mode, kind, blob = meta.decode().split()
    name = encoded.decode()
    assert kind == 'blob' and mode in {'100644', '100755', '120000'}
    path = source / name
    if mode == '120000':
        assert path.is_symlink()
        raw = os.readlink(path).encode()
    else:
        assert path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(source)
        raw = path.read_bytes()
    canonical = raw
    attributes = None
    representation = 'canonical-Git-bytes'
    if hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest() != blob:
        fields = subprocess.check_output(['git', 'check-attr', '-z', 'text', 'eol', '--', name], cwd=source, env=env).decode().split('\0')
        assert fields[-1] == '' and len(fields) == 7
        attributes = {fields[i + 1]: fields[i + 2] for i in range(0, len(fields) - 1, 3)}
        assert all(fields[i] == name for i in range(0, len(fields) - 1, 3))
        assert attributes == {'text': 'auto', 'eol': 'crlf'} or attributes == {'text': 'set', 'eol': 'crlf'}, name
        canonical = subprocess.check_output(['git', 'cat-file', 'blob', blob], cwd=source, env=env)
        assert b'\r\n' not in canonical and raw == canonical.replace(b'\n', b'\r\n'), name
        representation = 'publisher-declared-Git-eol-crlf'
    assert hashlib.sha1(b'blob ' + str(len(canonical)).encode() + b'\0' + canonical).hexdigest() == blob, name
    payloads[name] = {'mode': mode, 'blob': blob, 'sha256': hashlib.sha256(canonical).hexdigest(),
                      'canonical_bytes': len(canonical), 'checkout_sha256': hashlib.sha256(raw).hexdigest(),
                      'checkout_bytes': len(raw), 'representation': representation, 'Git_attributes': attributes}
(reports / 'complete-publisher-payloads.json').write_text(json.dumps(payloads, indent=2) + '\n')
assert payloads['test/parallel/test-x509-escaping.js']['sha256'] == spec['known_X509_case_sha256']
assert not git('status', '--porcelain', '--untracked-files=no')
next_spec = json.loads((project / 'tools/node-native-sdk/source-spec.json').read_bytes())
old_spec = dict(next_spec)
next_spec.update(tag=spec['tag'], commit=spec['commit'], tag_object_sha=spec['tag_object'],
                 tag_signer_fingerprint=spec['expected_current_signer_fingerprint'],
                 signer_key_sha256=spec['current_signer_public_key_sha256'])
gyp_sha = payloads['deps/sqlite/sqlite.gyp']['sha256']
assert gyp_sha == next_spec['publisher_sqlite_gyp_sha256']
assert {k for k in next_spec if next_spec[k] != old_spec[k]} == {'tag', 'commit', 'tag_object_sha', 'tag_signer_fingerprint', 'signer_key_sha256'}
(reports / 'UNAPPLIED-source-spec.json').write_text(json.dumps(next_spec, indent=2) + '\n')
(reports / 'UNAPPLIED-release-signer.asc').write_bytes(current_key.read_bytes())
project_after = metadata.payloads()
assert project_before == project_after
(reports / 'HAI-source-after.json').write_text(json.dumps(project_after, indent=2) + '\n')
result = {'worker_head': os.environ['GITHUB_SHA'], 'publisher': spec['publisher'], 'tag': spec['tag'],
    'publisher_commit': spec['commit'], 'publisher_tree': spec['tree'], 'tag_object': spec['tag_object'],
    'tag_signature_verified_with_existing_pinned_official_public_key': True,
    'approved_signer_fingerprint': spec['expected_current_signer_fingerprint'],
    'signer_is_member_of_unchanged_previously_pinned_approved_active_bundle': True,
    'all_publisher_source_payloads_bound_and_unmodified': len(payloads),
    'all_HAI_payloads_unchanged': len(project_after), 'known_X509_case_unchanged': True,
    'publisher_declared_checkout_representation_differences': [name for name, record in payloads.items() if record['representation'] != 'canonical-Git-bytes'],
    'UNAPPLIED_target_spec_changes_only_five_release_identity_fields_and_public_key_asset': True,
    'compiler_package_manager_SDK_or_case_execution': False,
    'full_Node_SDK_or_latest_native_dependencies_or_runtime_acceptance': False,
    'known_case_exception_and_other_latest_component_gates_remain_unresolved': True}
(reports / 'release-provenance.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result))
