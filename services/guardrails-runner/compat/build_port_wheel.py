"""Build a source-bound offline Guardrails wheel without importing vendor code."""
from pathlib import Path, PurePosixPath
import argparse
import ast
import base64
import csv
import email
import hashlib
import io
import json
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener
import zipfile


def check_url(value):
    parsed = urlsplit(value)
    if (parsed.scheme != 'https' or parsed.hostname != 'files.pythonhosted.org'
            or parsed.username or parsed.password or parsed.port not in {None, 443}):
        raise ValueError('Upstream wheel URL is outside the approved public source')


class Redirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        check_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def guard_methods(data):
    source = data.decode('utf-8')
    cls = next(n for n in ast.parse(source).body if isinstance(n, ast.ClassDef) and n.name == 'Guard')
    lines = source.splitlines(keepends=True)
    result = {}
    for node in cls.body:
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            first = min([node.lineno] + [n.lineno for n in node.decorator_list])
            result[node.name] = sha256(''.join(lines[first-1:node.end_lineno]).encode())
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-directory', type=Path, required=True)
    parser.add_argument('--upstream-wheel', type=Path)
    args = parser.parse_args()
    spec = json.loads(Path(__file__).with_name('port-spec.json').read_text(encoding='utf-8'))
    if args.upstream_wheel:
        raw = args.upstream_wheel.read_bytes()
    else:
        check_url(spec['upstream_url'])
        with build_opener(Redirects()).open(spec['upstream_url'], timeout=60) as response:
            check_url(response.url)
            raw = response.read(8 * 1024 * 1024 + 1)
    if len(raw) > 8 * 1024 * 1024 or sha256(raw) != spec['upstream_wheel_sha256']:
        raise ValueError('Guardrails upstream wheel identity differs from the reviewed source')
    files = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist()) > 10000 or sum(v.file_size for v in archive.infolist()) > 64 * 1024 * 1024:
            raise ValueError('Unexpected upstream wheel bounds')
        for member in archive.infolist():
            path = PurePosixPath(member.filename)
            if path.is_absolute() or '..' in path.parts or '\\' in member.filename:
                raise ValueError('Unexpected upstream member path')
            if member.is_dir():
                continue
            files[member.filename] = archive.read(member)
    old_info = 'guardrails_ai-' + spec['upstream_version'] + '.dist-info'
    new_info = 'guardrails_ai-' + spec['local_version'] + '.dist-info'
    metadata = email.message_from_bytes(files[old_info + '/METADATA'])
    if metadata['Name'] != 'guardrails-ai' or metadata['Version'] != spec['upstream_version']:
        raise ValueError('Unexpected upstream distribution metadata')
    for patch in spec['patches']:
        original = files[patch['path']]
        replacement = patch['source'].encode('utf-8')
        if sha256(original) != patch['original_sha256'] or sha256(replacement) != patch['patched_sha256']:
            raise ValueError('Reviewed module identity differs: ' + patch['path'])
        ast.parse(replacement)
        files[patch['path']] = replacement
    for path, digest in spec['unchanged_validation_modules'].items():
        if sha256(files[path]) != digest:
            raise ValueError('Validation engine source changed: ' + path)
    if guard_methods(files['guardrails/guard.py']) != spec['unchanged_Guard_method_source_hashes']:
        raise ValueError('A Guard method source differs from the reviewed upstream engine')
    # Preserve upstream UTF-8 bytes and avoid platform/email/zlib-dependent
    # rewrites. The derived archive uses fixed metadata and stored members.
    original_metadata = files[old_info + '/METADATA'].replace(b'\r\n', b'\n')
    header, separator, payload = original_metadata.partition(b'\n\n')
    if not separator:
        raise ValueError('Unexpected upstream metadata envelope')
    kept = []
    skip = False
    for line in header.splitlines():
        if line.startswith((b' ', b'\t')):
            if not skip:
                kept.append(line)
            continue
        key = line.split(b':', 1)[0].lower()
        skip = key in {b'requires-dist', b'provides-extra'}
        if skip:
            continue
        if key == b'version':
            line = ('Version: ' + spec['local_version']).encode()
        elif key == b'requires-python':
            line = ('Requires-Python: ' + spec['requires_python']).encode()
        elif key == b'summary':
            line += b' (HAI offline compatibility port)'
        kept.append(line)
    kept.extend(('Requires-Dist: ' + value).encode() for value in spec['core_requirements'])
    notice = ('\n\nHAI compatibility profile: ' + spec['profile'] + '\n'
              'Local modifications retain the original validation engine and Apache-2.0 license.\n').encode()
    files[old_info + '/METADATA'] = b'\n'.join(kept) + b'\n\n' + payload + notice
    wheel_header = files[old_info + '/WHEEL'].decode('utf-8').splitlines()
    wheel_header = ['Generator: HAI offline compatibility wheel builder' if line.startswith('Generator:') else line for line in wheel_header]
    files[old_info + '/WHEEL'] = ('\n'.join(wheel_header) + '\n').encode()
    files.pop(old_info + '/entry_points.txt', None)
    files.pop(old_info + '/RECORD')
    files = {name.replace(old_info + '/', new_info + '/', 1): data for name, data in files.items()}
    receipt = {k: v for k, v in spec.items() if k != 'patches'}
    receipt['patches'] = [{k: v for k, v in patch.items() if k != 'source'} for patch in spec['patches']]
    receipt['vendor_code_executed_during_wheel_build'] = False
    files[new_info + '/HAI_PORT.json'] = (json.dumps(receipt, indent=2) + '\n').encode()
    record = io.StringIO(newline='')
    writer = csv.writer(record, lineterminator='\n')
    for name, data in sorted(files.items()):
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b'=').decode('ascii')
        writer.writerow([name, 'sha256=' + digest, len(data)])
    writer.writerow([new_info + '/RECORD', '', ''])
    files[new_info + '/RECORD'] = record.getvalue().encode()
    result = io.BytesIO()
    with zipfile.ZipFile(result, 'w', compression=zipfile.ZIP_STORED) as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_STORED
            archive.writestr(info, data)
    args.output_directory.mkdir(parents=True, exist_ok=True)
    output = args.output_directory / ('guardrails_ai-' + spec['local_version'] + '-py3-none-any.whl')
    with output.open('xb') as stream:
        stream.write(result.getvalue())
    print(json.dumps({'filename': output.name, 'sha256': sha256(result.getvalue()),
                      'upstream_sha256': spec['upstream_wheel_sha256'],
                      'patched_modules': len(spec['patches']),
                      'unchanged_Guard_methods': len(spec['unchanged_Guard_method_source_hashes']),
                      'vendor_code_executed': False, 'runtime_qualified': False}))


if __name__ == '__main__':
    main()
