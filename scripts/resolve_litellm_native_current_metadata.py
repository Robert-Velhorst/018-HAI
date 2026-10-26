"""Run normal hosted Cargo resolution and audit every actual public crate version."""
from pathlib import Path
import concurrent.futures
import hashlib
import json
import os
import re
import subprocess
import urllib.request


def public_latest(name):
    lower=name.lower()
    part=('1/'+lower if len(lower)==1 else '2/'+lower if len(lower)==2
        else '3/'+lower[0]+'/'+lower if len(lower)==3 else lower[:2]+'/'+lower[2:4]+'/'+lower)
    url='https://index.crates.io/'+part
    try:
        with urllib.request.urlopen(url,timeout=45) as response:raw=response.read(8*1024*1024+1)
        if len(raw)>8*1024*1024:raise ValueError('Index response exceeds bound')
        values=[json.loads(line) for line in raw.splitlines() if line]
        stable=[v for v in values if not v['yanked'] and re.fullmatch(r'\d+\.\d+\.\d+(?:\+[A-Za-z0-9._-]+)?',v['vers'])]
        latest=max(stable,key=lambda v:tuple(map(int,v['vers'].split('+')[0].split('.'))))
        return {'name':name,'source':url,'latest_stable':latest['vers'],
            'checksum':latest['cksum'],'index_sha256':hashlib.sha256(raw).hexdigest()}
    except Exception as exc:return {'name':name,'source':url,'error':str(exc)}


def main():
    if os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':
        raise RuntimeError('Use only the disposable hosted metadata worker')
    temporary=Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
    source=temporary/'hai-litellm-native-source';reports=temporary/'hai-litellm-native-reports'
    identity=json.loads((reports/'publisher-source-identity.json').read_text())
    environment={k:v for k,v in os.environ.items() if k in {
        'PATH','HOME','USER','LANG','LC_ALL','TMPDIR','CARGO_HOME','RUSTUP_HOME','SSL_CERT_FILE','SSL_CERT_DIR'}}
    toolchain=subprocess.check_output(['rustc','--version'],cwd=source,env=environment,text=True).strip()
    if not toolchain.startswith('rustc 1.99.1 '):raise ValueError('The reviewed current compiler is required')
    with (reports/'cargo-metadata.json').open('w') as output,(reports/'cargo-metadata.log').open('w') as log:
        result=subprocess.run(['cargo','metadata','--format-version','1','--all-features',
            '--manifest-path','litellm-rust/Cargo.toml'],cwd=source,env=environment,stdout=output,stderr=log)
    print((reports/'cargo-metadata.log').read_text(),flush=True)
    changed=[]
    for name,digest in identity['proposed_payload_sha256'].items():
        path=source/name
        payload=os.readlink(path).encode() if path.is_symlink() else path.read_bytes()
        if hashlib.sha256(payload).hexdigest()!=digest:changed.append(name)
    if any(name!='litellm-rust/Cargo.lock' for name in changed):
        raise ValueError('Cargo changed original SDK source or cases')
    (reports/'resolved-Cargo.lock').write_bytes((source/'litellm-rust/Cargo.lock').read_bytes())
    packages=[];queries=[];gaps=[];unknown_sources=[];prereleases=[];complete=False
    if result.returncode==0:
        metadata=json.loads((reports/'cargo-metadata.json').read_text())
        packages=metadata['packages'];registry=[p for p in packages if p.get('source','') and p['source'].startswith('registry+')]
        complete=bool(registry and metadata.get('workspace_members') and metadata.get('resolve')
            and {p['id'] for p in packages}=={n['id'] for n in metadata['resolve']['nodes']})
        prereleases=[{'name':p['name'],'version':p['version']} for p in packages
            if not re.fullmatch(r'\d+\.\d+\.\d+(?:\+[A-Za-z0-9._-]+)?',p['version'])]
        names=sorted({p['name'] for p in registry})
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:queries=list(pool.map(public_latest,names))
        public={r['name']:r for r in queries}
        for package in registry:
            row=public[package['name']]
            if row.get('error') or package['version']!=row['latest_stable']:
                gaps.append({'name':package['name'],'resolved':package['version'],
                    'latest_stable':row.get('latest_stable'),'query_error':row.get('error')})
        unknown_sources=[{'name':p['name'],'source':p['source']} for p in packages
            if p.get('source') and not p['source'].startswith('registry+')]
    record={'source_head':os.environ['GITHUB_SHA'],'compiler':toolchain,
        'normal_Cargo_metadata_returncode':result.returncode,'resolved_packages':len(packages),
        'public_crate_queries':queries,'noncurrent_or_unknown_registry_versions':gaps,
        'other_external_sources':unknown_sources,'prerelease_versions':prereleases,
        'complete_resolve_node_inventory_verified':complete,
        'original_payloads_verified':len(identity['proposed_payload_sha256']),
        'only_original_Cargo_lock_updated_by_normal_resolver':changed,
        'recursive_current_stable_graph_qualified':result.returncode==0 and complete and not gaps and not unknown_sources and not prereleases,
        'native_compilation_original_suites_image_or_deployment_acceptance':False}
    (reports/'resolution.json').write_text(json.dumps(record,indent=2)+'\n')
    if not record['recursive_current_stable_graph_qualified']:
        raise SystemExit('Normal resolver/current complete graph remains unqualified; see retained evidence')


if __name__=='__main__':main()
