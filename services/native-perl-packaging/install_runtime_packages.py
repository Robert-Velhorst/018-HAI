"""Install three real, authenticated GCC runtime packages in disposable images."""
from pathlib import Path
import hashlib,json,os,platform,pwd,re,subprocess,sys,time


def main():
    if (sys.platform!='linux' or os.getuid()!=0 or platform.python_version()!='3.15.1'
            or os.environ.get('HAI_GCC_RUNTIME_SDK_BUILD')!='github-hosted'):
        raise RuntimeError('Use only the disposable image preparation stage')
    folder=Path(__file__).resolve().parent
    spec=json.loads((folder/'runtime-sources.json').read_text())
    expected={'gcc-16-base','libgcc-s1','libstdc++6'}
    if ({p['package'] for p in spec['packages']}!=expected or len(spec['packages'])!=3
            or spec['version']!='16.2.0-3' or spec['distribution']!='sid'
            or spec['upstream_release']!='16.2.0' or spec['architecture']!='amd64'):
        raise ValueError('Unexpected compiler runtime scope')
    output=Path('/usr/share/doc/hai-gcc-runtime-setup');output.mkdir(parents=True,exist_ok=True)
    work=Path('/tmp/hai-gcc16-runtime-downloads');work.mkdir(exist_ok=False)
    sandbox=pwd.getpwnam('_apt');os.chown(work,sandbox.pw_uid,sandbox.pw_gid)
    repository=Path('/etc/apt/sources.list.d/hai-gcc16-runtime.list')
    preferences=Path('/etc/apt/preferences.d/hai-gcc16-runtime')
    if repository.exists() or preferences.exists():raise ValueError('Do not replace an existing package policy')
    repository.write_text('deb [arch=amd64 signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://deb.debian.org/debian sid main\n')
    preferences.write_text('Package: gcc-16-base libgcc-s1 libstdc++6\nPin: version 16.2.0-3\nPin-Priority: 990\n\n'
                           'Package: *\nPin: release n=sid\nPin-Priority: -1\n')
    environment={'PATH':'/usr/local/bin:/usr/bin:/bin','HOME':'/root','LANG':'C.UTF-8','LC_ALL':'C.UTF-8',
                 'DEBIAN_FRONTEND':'noninteractive','LD_LIBRARY_PATH':'/usr/local/lib64:/usr/local/lib'}
    sha=lambda raw:hashlib.sha256(raw).hexdigest()
    report={'scope':spec['scope'],'distribution':'sid','upstream_release':'16.2.0',
            'spec_sha256':sha((folder/'runtime-sources.json').read_bytes()),'commands':[],
            'normal_authenticated_APT_and_exact_package_install_passed':False,
            'custom_package_ownership_provides_or_dependency_metadata_authored':False,
            'operator_or_server_or_application_image_changed':False}
    def run(name,command,timeout=300):
        path=output/(name+'.log');entry={'name':name,'argv':command};report['commands'].append(entry)
        started=time.monotonic()
        try:
            with path.open('wb') as handle:
                result=subprocess.run(command,cwd=work,env=environment,stdout=handle,stderr=subprocess.STDOUT,timeout=timeout)
            entry['returncode']=result.returncode
            if result.returncode:
                print(path.read_text(errors='replace')[-12000:],file=sys.stderr)
                raise RuntimeError(name+' failed under normal APT policy')
            return path.read_text(errors='replace')
        finally:
            entry['seconds']=round(time.monotonic()-started,2)
            if path.exists():entry['log_sha256']=sha(path.read_bytes())
    try:
        if run('architecture',['dpkg','--print-architecture']).strip()!='amd64':raise RuntimeError('Unexpected architecture')
        run('authenticated-index-refresh',['apt-get','update'],600)
        selections=[p['package']+'='+p['version'] for p in spec['packages']]
        run('authenticated-runtime-download',['apt-get','download',*selections],600)
        paths=[]
        for package in spec['packages']:
            name=Path(package['filename']).name
            if package['version']!=spec['version'] or package['source']!='gcc-16' or '/' in name:
                raise ValueError('Unexpected runtime package identity')
            path=work/name;raw=path.read_bytes()
            if len(raw)!=package['bytes'] or sha(raw)!=package['sha256']:raise ValueError('Authenticated package differs from reviewed hash')
            fields=run(package['package']+'-control',['dpkg-deb','--field',str(path)])
            for field,value in [('Package',package['package']),('Version',package['version']),('Architecture','amd64')]:
                if re.search(r'(?m)^'+field+r': (.+)$',fields)[1]!=value:raise ValueError('Runtime package control differs')
            paths.append(str(path))
        run('runtime-no-remove-simulation',['apt-get','--simulate','--no-remove','install',*paths])
        run('normal-runtime-install',['apt-get','-y','--no-remove','install',*paths],600)
        status=run('exact-runtime-status',['dpkg-query','-W','-f=${binary:Package}\t${Version}\t${db:Status-Status}\n',*sorted(expected)])
        actual={row[0].split(':')[0]:row[1:] for row in (line.split('\t') for line in status.splitlines()) if len(row)==3}
        if actual!={name:[spec['version'],'installed'] for name in expected}:raise RuntimeError('Exact runtime family is not configured')
        if run('runtime-dpkg-audit',['dpkg','--audit']).strip():raise RuntimeError('Package state is incomplete')
        report['owned_runtime_files']={}
        for package,filename in [('libgcc-s1','libgcc_s.so.1'),('libstdc++6','libstdc++.so.6')]:
            path=Path('/usr/lib/x86_64-linux-gnu')/filename
            owner=run(package+'-ownership',['dpkg-query','--search',str(path)]).strip()
            if not owner.startswith(package+':'):raise RuntimeError('Runtime path lacks real package ownership')
            dynamic=run(package+'-SONAME',['readelf','-d',str(path)])
            if 'Library soname: ['+filename+']' not in dynamic:raise RuntimeError('Runtime SONAME differs')
            report['owned_runtime_files'][filename]={'path':str(path),'resolved_path':str(path.resolve()),
                                                    'sha256':sha(path.read_bytes()),'package_owner':owner}
        report['normal_authenticated_APT_and_exact_package_install_passed']=True
    finally:
        report['output_file_sha256']={p.name:sha(p.read_bytes()) for p in output.iterdir()
                                     if p.is_file() and p.name!='runtime-qualification.json'}
        (output/'runtime-qualification.json').write_text(json.dumps(report,indent=2)+'\n')


if __name__=='__main__':main()
