"""Construct the hash-bound FastUUID source port without importing vendor code."""
from pathlib import Path, PurePosixPath
import argparse
import hashlib
import io
import json
import tarfile
import urllib.request


def prepare(output, archive=None):
    spec = json.loads(Path(__file__).with_name('fastuuid-port-spec.json').read_text())
    raw = archive.read_bytes() if archive else urllib.request.urlopen(spec['source_url'], timeout=60).read()
    if len(raw) != spec['source_bytes'] or hashlib.sha256(raw).hexdigest() != spec['source_sha256']:
        raise ValueError('FastUUID source archive identity differs')
    entries = {}
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as tar:
        for member in tar:
            if member.isdir():
                continue
            if not member.isfile() or member.size > 8 * 1024 * 1024:
                raise ValueError('Unexpected source archive member')
            path = PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or len(path.parts) < 2:
                raise ValueError('Source archive path escapes its root')
            name = '/'.join(path.parts[1:])
            if name in entries:
                raise ValueError('Duplicate source path')
            entries[name] = (tar.extractfile(member).read(), member.mode & 0o777)
    manifest = []
    for name, (original, mode) in list(entries.items()):
        value = original
        if name in spec['patches']:
            patch = spec['patches'][name]
            if hashlib.sha256(original).hexdigest() != patch['original_sha256']:
                raise ValueError('Original source differs: ' + name)
            for edit in patch['edits']:
                before, after = edit['old'].encode(), edit['new'].encode()
                if value.count(before) != 1:
                    raise ValueError('Source edit is ambiguous: ' + name)
                value = value.replace(before, after, 1)
            if hashlib.sha256(value).hexdigest() != patch['patched_sha256']:
                raise ValueError('Ported source differs: ' + name)
        entries[name] = (value, mode)
        manifest.append({'path': name, 'original_sha256': hashlib.sha256(original).hexdigest(),
                         'ported_sha256': hashlib.sha256(value).hexdigest(), 'mode': mode})
    if not set(spec['patches']).issubset(entries):
        raise ValueError('Missing source patch target')
    output.mkdir(parents=True, exist_ok=False)
    for name, (value, mode) in entries.items():
        # Preserve the upstream lock as evidence; the candidate lock is generated
        # by the current Cargo only inside the hosted disposable build.
        destination = output / ('Cargo.lock.upstream' if name == 'Cargo.lock' else name)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(value)
        destination.chmod(mode)
    receipt = {'upstream_source_sha256': spec['source_sha256'],
               'local_version': spec['local_version'], 'files': manifest,
               'modified_files': sorted(spec['patches']),
               'vendor_code_imported_or_executed': False,
               'upstream_cases_preserved': all(row['original_sha256'] == row['ported_sha256']
                                               for row in manifest if row['path'].startswith('tests/'))}
    (output / 'HAI_SOURCE_PORT.json').write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--archive', type=Path)
    args = parser.parse_args()
    result = prepare(args.output, args.archive)
    print(json.dumps({'source_files': len(result['files']), 'modified_files': result['modified_files'],
                      'upstream_cases_preserved': result['upstream_cases_preserved']}))
