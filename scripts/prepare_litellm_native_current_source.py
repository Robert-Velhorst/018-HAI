"""Prepare the complete hash-bound public SDK and transparent manifest overlays."""
from pathlib import Path, PurePosixPath
import hashlib
import json
import os
import tarfile
import urllib.request


def main():
    if os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted' or os.name != 'posix':
        raise RuntimeError('Use the disposable hosted public metadata worker')
    temporary=Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
    source=temporary/'hai-litellm-native-source';reports=temporary/'hai-litellm-native-reports'
    if source.exists():raise ValueError('Refuse an existing source working directory')
    source.mkdir();reports.mkdir(exist_ok=True)
    inputs=Path(__file__).resolve().parents[1]/'services/miniswe-runner/compat/litellm-native-current'
    spec=json.loads((inputs/'source-spec.json').read_text())
    archive=temporary/'hai-litellm-publisher.tar.gz'
    digest=hashlib.sha256();total=0
    with urllib.request.urlopen(spec['archive_url'],timeout=90) as response, archive.open('xb') as target:
        while chunk:=response.read(1024*1024):
            total+=len(chunk)
            if total>512*1024*1024:raise ValueError('Publisher archive size exceeded')
            digest.update(chunk);target.write(chunk)
    if digest.hexdigest()!=spec['archive_sha256']:raise ValueError('Publisher archive identity differs')
    hashes={};links=[];source_bytes=0
    with tarfile.open(archive,mode='r:gz') as tar:
        for member in tar:
            if member.isdir():continue
            parts=PurePosixPath(member.name).parts
            if member.name.startswith('/') or '..' in parts or len(parts)<2:
                raise ValueError('Unexpected publisher member path')
            name='/'.join(parts[1:]);path=source/name
            if name in hashes or member.size>64*1024*1024 or not path.resolve().is_relative_to(source):
                raise ValueError('Duplicate, oversized or escaping publisher member')
            if member.issym():
                data=member.linkname.encode();links.append((path,member.linkname))
            else:
                if not member.isfile():raise ValueError('Unsupported publisher archive member')
                data=tar.extractfile(member).read();path.parent.mkdir(parents=True,exist_ok=True)
                path.write_bytes(data)
                if member.mode & 0o111:path.chmod(0o755)
            hashes[name]=hashlib.sha256(data).hexdigest();source_bytes+=len(data)
    if len(hashes)!=spec['source_blob_count'] or source_bytes!=spec['source_total_bytes']:
        raise ValueError('Complete publisher source inventory differs')
    if (hashes.get('LICENSE')!=spec['publisher_license_sha256']
            or hashlib.sha256((inputs/'LICENSE.upstream').read_bytes()).hexdigest()!=hashes['LICENSE']):
        raise ValueError('Original publisher license must be preserved')
    for path,link in links:
        if Path(link).is_absolute() or not (path.parent/link).resolve().is_relative_to(source):
            raise ValueError('Publisher symlink escapes source')
        path.parent.mkdir(parents=True,exist_ok=True);path.symlink_to(link)
    original=dict(hashes)
    for overlay in spec['overlays']:
        name=overlay['path']
        if hashes.get(name)!=overlay['original_sha256']:raise ValueError('Original overlay source differs')
        payload=(inputs/'overlays'/name).read_bytes()
        if hashlib.sha256(payload).hexdigest()!=overlay['proposed_sha256']:
            raise ValueError('Reviewed overlay bytes differ')
        (source/name).write_bytes(payload);hashes[name]=overlay['proposed_sha256']
    (reports/'publisher-Cargo.lock').write_bytes((source/'litellm-rust/Cargo.lock').read_bytes())
    (reports/'publisher-source-identity.json').write_text(json.dumps({
        'source_head':os.environ['GITHUB_SHA'],'publisher_commit':spec['commit'],
        'publisher_archive_sha256':spec['archive_sha256'],'original_payload_sha256':original,
        'proposed_payload_sha256':hashes,'overlay_count':len(spec['overlays']),
        'original_cases_helpers_assertions_and_source_unchanged_except_declared_manifests':True,
        'metadata_only_not_compilation_or_suite_acceptance':True},indent=2)+'\n')
    print(json.dumps({'publisher_payloads_verified':len(hashes),'overlays':len(spec['overlays'])}))


if __name__=='__main__':main()
