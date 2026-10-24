"""Build a source-bound ACL package candidate only in the disposable hosted SDK."""
from pathlib import Path
import collections,hashlib,json,os,platform,re,shutil,subprocess,sys,tarfile,time

def main():
    if (sys.platform!='linux' or os.getuid()!=10001 or os.environ.get('HAI_DISPOSABLE_ACL_SOURCE_SDK')!='1' or os.environ.get('HAI_HOSTED_SOURCE_QUALIFICATION')!='github-hosted'):
        raise RuntimeError('Use only the dedicated non-root hosted ACL SDK')
    if platform.python_version()!='3.15.1':raise RuntimeError('Unexpected interpreter')
    compiler=subprocess.check_output(['gcc','-dumpfullversion'],text=True).strip()
    if compiler!='16.2.0':raise RuntimeError('Unexpected compiler')
    sha=lambda raw:hashlib.sha256(raw).hexdigest()
    work,output=Path('/build'),Path('/outputs');spec=json.loads(Path('/inputs/sources.json').read_text())
    (work/'home').mkdir();(work/'tmp').mkdir()
    for item in spec['inputs']:
        raw=(Path('/inputs')/item['name']).read_bytes()
        if sha(raw)!=item['sha256'] or len(raw)!=item['bytes']:raise ValueError('Public input changed')
        shutil.copyfile(Path('/inputs')/item['name'],work/item['name'])
    for name,target in [('acl_2.4.0.orig.tar.xz',work),('acl_2.4.0-1.debian.tar.xz',work/spec['archive_root'])]:
        with tarfile.open(work/name,mode='r:xz') as tar:
            members=tar.getmembers()
            if len(members)>10000 or sum(m.size for m in members)>64*1024*1024:raise ValueError('Unexpected source member bounds')
            tar.extractall(target,filter='data')
    source=work/spec['archive_root']
    cases={p.relative_to(source).as_posix():sha(p.read_bytes()) for p in (source/'test').rglob('*') if p.is_file() and not p.is_symlink()}
    originals={p.relative_to(source).as_posix():sha(p.read_bytes()) for p in source.rglob('*') if p.is_file() and not p.is_symlink() and (p.suffix in {'.c','.h','.am','.ac'} or p.relative_to(source).as_posix() in spec['expected_source_patches']) and not p.relative_to(source).as_posix().startswith('debian/')}
    env={'PATH':os.environ['PATH'],'HOME':'/build/home','TMPDIR':'/build/tmp','LANG':'C.UTF-8','LC_ALL':'C.UTF-8','TZ':'UTC','LD_LIBRARY_PATH':'/usr/local/lib64:/usr/local/lib','DEB_BUILD_OPTIONS':'parallel=2'}
    receipt={'version':spec['version'],'Python':platform.python_version(),'GCC':compiler,'UID':os.getuid(),'original_test_files':cases,'source_inputs':spec['inputs'],'build_command':['dpkg-buildpackage','--build=binary','--no-sign','--jobs=2'],'runtime_packages_installed':False,'complete_runtime_security_or_ABI_acceptance':False,'all_upstream_cases_qualified':False,'new_application_cases_or_test_exclusions':False,'signature_verified':False}
    try:
        start=time.monotonic()
        with (output/'package-build.log').open('wb') as handle:
            result=subprocess.run(receipt['build_command'],cwd=source,env=env,stdout=handle,stderr=subprocess.STDOUT,timeout=1800)
        receipt['build_returncode']=result.returncode;receipt['build_seconds']=round(time.monotonic()-start,2)
        log=(output/'package-build.log').read_text(errors='replace')
        receipt['case_status_lines']=[line for line in log.splitlines() if re.match(r'^(PASS|FAIL|SKIP|XFAIL|XPASS|ERROR):',line)]
        receipt['case_status_counts']={}
        for name in ['TOTAL','PASS','FAIL','SKIP','XFAIL','XPASS','ERROR']:
            values=re.findall(r'(?m)^#\s+'+name+r':\s+(\d+)\s*$',log)
            receipt['case_status_counts'][name]=int(values[-1]) if values else None
        if result.returncode:raise RuntimeError('Source package build failed; inspect the retained full log')
        packages=[]
        for package in sorted(work.glob('*.deb')):
            shutil.copyfile(package,output/package.name)
            fields=subprocess.check_output(['dpkg-deb','--field',str(package)],text=True)
            contents=subprocess.check_output(['dpkg-deb','--contents',str(package)],text=True)
            (output/(package.name+'.control.txt')).write_text(fields)
            (output/(package.name+'.contents.txt')).write_text(contents)
            packages.append({'name':package.name,'sha256':sha(package.read_bytes()),'control_sha256':sha(fields.encode()),'contents_sha256':sha(contents.encode())})
        if not any(p['name'].startswith('libacl1_2.4.0-1_amd64') for p in packages):raise ValueError('Expected complete libacl1 runtime package absent')
        receipt['packages']=packages
        for pattern in ['*.buildinfo','*.changes']:
            for item in work.glob(pattern):shutil.copyfile(item,output/item.name)
        # Retain the actual built source and logs, including the unchanged upstream cases.
        shutil.make_archive(str(output/'built-source'),'gztar',source.parent,source.name)
        counts=receipt['case_status_counts']
        receipt['all_upstream_cases_qualified']=bool(counts['PASS']) and all(counts[n]==0 for n in ['FAIL','SKIP','XFAIL','XPASS','ERROR'])
        if not receipt['all_upstream_cases_qualified']:raise RuntimeError('Candidate packages retained; skipped, expected-failing or unknown upstream cases remain unqualified')
    finally:
        states=[]
        for entry in Path('/proc').iterdir():
            if not entry.name.isdigit():continue
            try:states.append((entry/'stat').read_text().rsplit(')',1)[1].split()[0])
            except (FileNotFoundError,ProcessLookupError,PermissionError):continue
        receipt['process_states_at_exit']=dict(collections.Counter(states))
        receipt['cgroup_process_counts_at_exit']={name:(Path('/sys/fs/cgroup')/name).read_text().strip() for name in ['pids.current','pids.peak','pids.max'] if (Path('/sys/fs/cgroup')/name).is_file()}
        changed_cases=[n for n,h in cases.items() if not (source/n).is_file() or sha((source/n).read_bytes())!=h]
        changed_source=[n for n,h in originals.items() if not (source/n).is_file() or sha((source/n).read_bytes())!=h]
        receipt['original_test_files_changed']=changed_cases;receipt['original_source_files_changed']=changed_source
        receipt['source_changes_outside_reviewed_Debian_patches']=sorted(set(changed_source)-set(spec['expected_source_patches']))
        for item in source.rglob('*'):
            if item.is_file() and not item.is_symlink() and item.resolve().is_relative_to(source.resolve()) and (item.suffix=='.trs' or item.name=='test-suite.log'):
                target=output/'upstream-case-logs'/item.relative_to(source)
                target.parent.mkdir(parents=True,exist_ok=True)
                shutil.copyfile(item,target)
        receipt['output_file_sha256']={p.name:sha(p.read_bytes()) for p in output.iterdir() if p.is_file() and p.name!='qualification.json'}
        (output/'qualification.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
        if changed_cases or receipt['source_changes_outside_reviewed_Debian_patches']:raise RuntimeError('Original cases or unreviewed upstream source changed')
if __name__=='__main__':main()
