"""Prepare explicit hash-bound crate ports without executing Rust or vendor code."""
from pathlib import PurePosixPath
import hashlib
import io
import json
import tarfile
import urllib.request
from urllib.parse import urlsplit


def prepare_components(output, components):
    receipts = []
    output.mkdir(parents=True, exist_ok=False)
    for component in components:
        if component['name'] not in {'uuid','getrandom'}:
            raise ValueError('Unexpected source component')
        url = urlsplit(component['source_url'])
        if (url.scheme != 'https' or url.hostname != 'static.crates.io'
                or url.username or url.password or url.port not in {None,443}
                or not url.path.startswith('/crates/'+component['name']+'/')):
            raise ValueError('Crate source must use the fixed public registry')
        raw = urllib.request.urlopen(component['source_url'],timeout=60).read()
        if len(raw) != component['source_bytes'] or hashlib.sha256(raw).hexdigest() != component['source_sha256']:
            raise ValueError('Crate source archive differs: '+component['name'])
        entries = {}
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as tar:
            for member in tar:
                if member.isdir(): continue
                if not member.isfile() or member.size > 8*1024*1024:
                    raise ValueError('Unexpected crate member')
                path = PurePosixPath(member.name)
                if path.is_absolute() or '..' in path.parts or len(path.parts)<2:
                    raise ValueError('Crate path escapes source root')
                name = '/'.join(path.parts[1:])
                if name in entries: raise ValueError('Duplicate crate member')
                entries[name] = (tar.extractfile(member).read(),member.mode & 0o777)
        if not set(component['patches']).issubset(entries):
            raise ValueError('Missing reviewed crate patch target')
        manifest = []
        for name,(original,mode) in list(entries.items()):
            value = original
            if name in component['patches']:
                patch = component['patches'][name]
                if hashlib.sha256(original).hexdigest() != patch['original_sha256']:
                    raise ValueError('Original crate file differs')
                for edit in patch['edits']:
                    before,after = edit['old'].encode(),edit['new'].encode()
                    if value.count(before) != 1: raise ValueError('Ambiguous crate source edit')
                    value = value.replace(before,after,1)
                if hashlib.sha256(value).hexdigest() != patch['patched_sha256']:
                    raise ValueError('Ported crate file differs')
            entries[name] = (value,mode)
            manifest.append({'path':name,'original_sha256':hashlib.sha256(original).hexdigest(),
                             'ported_sha256':hashlib.sha256(value).hexdigest(),'mode':mode})
        # Every Rust implementation and upstream case remains byte-identical.
        if any(r['original_sha256'] != r['ported_sha256'] for r in manifest if r['path'].endswith('.rs')):
            raise ValueError('An unreviewed Rust implementation changed')
        folder = output/component['name']
        folder.mkdir()
        for name,(value,mode) in entries.items():
            target = folder/('Cargo.lock.upstream' if name=='Cargo.lock' else name)
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(value)
            target.chmod(mode)
        receipt = {'name':component['name'],'upstream_version':component['upstream_version'],
                   'local_version':component['local_version'],'source_sha256':component['source_sha256'],
                   'files':manifest,'modified_files':sorted(component['patches']),
                   'all_Rust_implementation_and_existing_cases_unchanged':True,
                   'vendor_code_executed':False,'runtime_or_cross_platform_qualified':False}
        (folder/'HAI_CRATE_PORT.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
        receipts.append(receipt)
    return receipts
