"""Run original ACL cases against exact installed candidates in an isolated SDK."""
from pathlib import Path
import hashlib,io,json,os,platform,re,subprocess,sys,tarfile


def verify_packages(folder, receipt_path):
    receipt=json.loads(receipt_path.read_text(encoding='utf-8'))
    if receipt['build_returncode']!=0:raise ValueError('Normal package build must pass first')
    expected={row['name']:row['sha256'] for row in receipt['packages']}
    if {p.name for p in folder.glob('*.deb')}!=set(expected):raise ValueError('Package set differs from built inputs')
    for name,digest in expected.items():
        if Path(name).name!=name or hashlib.sha256((folder/name).read_bytes()).hexdigest()!=digest:raise ValueError('Candidate package changed')
    return receipt


def main():
    if sys.argv[1:]==['--verify-package-inputs']:
        verify_packages(Path('/candidates'),Path('/review/build-qualification.json'))
        return
    if (sys.platform!='linux' or os.getuid()!=0
            or os.environ.get('HAI_DISPOSABLE_ACL_INSTALLED_SDK')!='1'
            or os.environ.get('HAI_HOSTED_SOURCE_QUALIFICATION')!='github-hosted'):
        raise RuntimeError('Use only the dedicated installed-package hosted SDK')
    if platform.python_version()!='3.15.1' or subprocess.check_output(['gcc','-dumpfullversion'],text=True).strip()!='16.2.0':raise ValueError('Unexpected SDK interpreter/compiler')
    inputs,build,work,output=Path('/inputs'),Path('/public-build'),Path('/root-cases'),Path('/case-reports')
    sha=lambda raw:hashlib.sha256(raw).hexdigest()
    spec=json.loads((inputs/'sources.json').read_text())
    for item in spec['inputs']:
        if sha((inputs/item['name']).read_bytes())!=item['sha256']:raise ValueError('Original public source input changed')
    receipt=verify_packages(build,build/'qualification.json')
    archive=build/'built-source.tar.gz'
    if sha(archive.read_bytes())!=receipt['output_file_sha256'][archive.name]:raise ValueError('Built source archive changed')
    (work/'home').mkdir();(work/'tmp').mkdir()
    with tarfile.open(archive,mode='r:gz') as tar:
        members=tar.getmembers()
        if len(members)>30000 or sum(m.size for m in members)>256*1024*1024:raise ValueError('Built-source member bounds')
        tar.extractall(work,filter='data')
    source=work/spec['archive_root']
    for name,digest in receipt['original_test_files'].items():
        if sha((source/name).read_bytes())!=digest:raise ValueError('An original upstream case/helper changed')
    selected=set()
    for line in receipt['case_status_lines']:
        match=re.fullmatch(r'(?:PASS|FAIL|SKIP|XFAIL|XPASS|ERROR): (test/[\w./-]+\.run)',line)
        if match:selected.add(match[1])
    if len(selected)!=16 or selected!={n for n in receipt['original_test_files'] if n.endswith('.run')}:raise ValueError('Full original Linux case set is missing')
    module=(source/'test/Makemodule.am').read_text()
    block=module.split('XFAIL_TESTS =',1)[1].split('\nTESTS =',1)[0]
    expected_failures=set(re.findall(r'test/[\w./-]+\.run',block))
    if expected_failures!={'test/nfs/nfsacl.run','test/nfs/nfs-dir.run'}:raise ValueError('Publisher expected-failure declarations changed')
    env={'PATH':'/usr/local/bin:/usr/bin:/bin','HOME':'/root-cases/home','TMPDIR':'/root-cases/tmp','USER':'root','LOGNAME':'root','LANG':'C.UTF-8','LC_ALL':'C.UTF-8','TZ':'UTC','srcdir':str(source),'LD_LIBRARY_PATH':'/usr/local/lib64:/usr/local/lib'}
    installed=[]
    for name in ['acl','libacl1:amd64']:
        version=subprocess.check_output(['dpkg-query','-W','-f=${Version}',name],text=True).strip()
        if version!='2.4.0-1':raise ValueError('Candidate package is not installed: '+name)
        installed.append({'package':name,'version':version})
    installed_files={}
    for name in ['acl_2.4.0-1_amd64.deb','libacl1_2.4.0-1_amd64.deb']:
        raw=subprocess.check_output(['dpkg-deb','--fsys-tarfile',str(build/name)])
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:') as tar:
            for item in tar.getmembers():
                if not item.isfile():continue
                path=Path('/')/item.name.removeprefix('./')
                if not path.is_relative_to('/usr'):raise ValueError('Unexpected candidate file prefix')
                digest=sha(tar.extractfile(item).read())
                if sha(path.read_bytes())!=digest:raise ValueError('Installed candidate file differs: '+str(path))
                installed_files[str(path)]=digest
    results=[]
    report={'installed_packages':installed,'installed_files_sha256':installed_files,'case_profile':'All 16 original Linux cases against exact installed ACL candidates','publisher_expected_failure_cases':sorted(expected_failures),'new_case_exclusions_or_application_cases':False,'runtime_or_operator_installation':False,'full_application_image_or_security_acceptance':False}
    try:
        for name in sorted(selected):
            log=output/(name.replace('/','__')+'.log')
            with log.open('wb') as handle:
                result=subprocess.run(['/bin/bash',str(source/'test/runwrapper'),name],cwd=source,env=env,stdout=handle,stderr=subprocess.STDOUT,timeout=120)
            status=('SKIP' if result.returncode==77 else 'ERROR' if result.returncode==99 else
                    'XPASS' if result.returncode==0 and name in expected_failures else
                    'XFAIL' if result.returncode!=0 and name in expected_failures else
                    'PASS' if result.returncode==0 else 'FAIL')
            results.append({'case':name,'returncode':result.returncode,'status':status,'log_sha256':sha(log.read_bytes())})
        counts={name:sum(r['status']==name for r in results) for name in ['PASS','SKIP','XFAIL','XPASS','FAIL','ERROR']}
        report['counts']=counts
        report['all_selected_cases_executed']=len(results)==16 and counts['SKIP']==0
        report['publisher_expected_outcomes_matched']=(counts=={'PASS':14,'SKIP':0,'XFAIL':2,'XPASS':0,'FAIL':0,'ERROR':0})
        report['all_cases_passed']=counts['PASS']==16
        if not report['publisher_expected_outcomes_matched']:raise RuntimeError('Unexpected original upstream case outcomes; inspect retained logs')
    finally:
        changed=[n for n,h in receipt['original_test_files'].items() if not (source/n).is_file() or sha((source/n).read_bytes())!=h]
        report['original_case_or_helper_files_changed']=changed;report['cases']=results
        report['cgroup_process_counts_at_exit']={name:(Path('/sys/fs/cgroup')/name).read_text().strip() for name in ['pids.current','pids.peak','pids.max'] if (Path('/sys/fs/cgroup')/name).is_file()}
        (output/'installed-case-qualification.json').write_text(json.dumps(report,indent=2)+'\n',encoding='utf-8')
        if changed:raise RuntimeError('An original upstream case/helper changed during qualification')


if __name__=='__main__':main()
