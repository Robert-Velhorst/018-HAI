"""Fetch and bind complete public PyYAML inputs without importing vendor code."""
from pathlib import Path, PurePosixPath
import argparse
import hashlib
import io
import json
import os
import tarfile
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    if os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted':
        raise RuntimeError('Preparation requires a disposable GitHub-hosted worker')
    output = args.output.resolve()
    temporary = Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
    if not output.is_relative_to(temporary) or output == temporary or output.exists():
        raise ValueError('Expected a new owned hosted temporary directory')
    spec = json.loads((Path(__file__).resolve().parents[1] /
        'services/miniswe-runner/compat/pyyaml-sdk-source.json').read_text())
    request = urllib.request.Request(spec['archive_url'], headers={'User-Agent': 'HAI-public-SDK-review'})
    with urllib.request.urlopen(request, timeout=60) as response:
        archive = response.read(8 * 1024 * 1024 + 1)
    if len(archive) > 8 * 1024 * 1024 or hashlib.sha256(archive).hexdigest() != spec['archive_sha256']:
        raise ValueError('Publisher archive identity differs')
    output.mkdir()
    found = {}
    with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as source:
        for member in source.getmembers():
            if member.isdir():
                continue
            parts = PurePosixPath(member.name).parts
            if not member.isfile() or len(parts) < 2 or any(p in {'..', '.'} for p in parts):
                raise ValueError('Unexpected publisher archive member')
            name = '/'.join(parts[1:])
            expected = spec['payload_sha256'].get(name)
            if not expected or name in found or member.size > 8 * 1024 * 1024:
                raise ValueError('Unknown, duplicate or oversized publisher member')
            data = source.extractfile(member).read()
            if hashlib.sha256(data).hexdigest() != expected:
                raise ValueError('Publisher source bytes differ: ' + name)
            target = output / name
            if not target.resolve().is_relative_to(output):
                raise ValueError('Publisher member escapes output')
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            found[name] = expected
    if found != spec['payload_sha256']:
        raise ValueError('Incomplete publisher source')
    (output / 'hai-source-identity.json').write_text(json.dumps({
        'publisher_commit': spec['commit'], 'archive_sha256': spec['archive_sha256'],
        'payload_sha256': found, 'all_publisher_source_verified': True,
        'vendor_code_executed_during_preparation': False}, indent=2) + '\n')
    print(json.dumps({'complete_publisher_payloads': len(found), 'commit': spec['commit']}))


if __name__ == '__main__':
    main()
