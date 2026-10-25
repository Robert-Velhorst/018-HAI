"""Verify a qualified library context before and after image installation."""
from pathlib import Path,PurePosixPath
import hashlib,json,subprocess,sys

def main():
    root=Path(sys.argv[1]);manifest=json.loads((root/'qualification.json').read_text(encoding='utf-8'))
    if manifest['schema']!=1 or manifest['package']!='libacl1' or manifest['version']!='2.4.0-1':raise ValueError('Unexpected qualified library profile')
    if manifest['installed_vendor_case_counts']!={'PASS':14,'SKIP':0,'XFAIL':2,'XPASS':0,'FAIL':0,'ERROR':0}:raise ValueError('Original installed vendor cases have not qualified')
    if not manifest['all_baseline_public_symbol_versions_preserved']:raise ValueError('Baseline public ELF surface has not qualified')
    archive=root/'libacl1_2.4.0-1_amd64.deb'
    if hashlib.sha256(archive.read_bytes()).hexdigest()!=manifest['package_sha256']:raise ValueError('Qualified package changed')
    if hashlib.sha256(Path(__file__).read_bytes()).hexdigest()!=manifest['verifier_sha256']:raise ValueError('Qualification verifier changed')
    release=dict(line.split('=',1) for line in Path('/etc/os-release').read_text().splitlines() if '=' in line)
    if release.get('ID','').strip('"')!='debian' or release.get('VERSION_ID','').strip('"').split('.')[0]!='13':raise ValueError('Qualified library requires Debian 13')
    installed=subprocess.check_output(['dpkg-query','-W','-f=${Version}','libacl1:amd64'],text=True).strip()
    if '--installed' in sys.argv:
        if installed!=manifest['version']:raise ValueError('Qualified package is not installed')
        for name,digest in manifest['payload_files_sha256'].items():
            relative=PurePosixPath(name)
            if relative.is_absolute() or '..' in relative.parts or relative.parts[0]!='usr':raise ValueError('Unexpected qualified package path')
            if hashlib.sha256(Path('/').joinpath(*relative.parts).read_bytes()).hexdigest()!=digest:raise ValueError('Installed qualified package file changed')
    elif installed not in {manifest['baseline_package_version'],manifest['version']}:
        raise ValueError('Image library baseline differs from the qualified ELF baseline; rebuild the candidate')
    print(json.dumps({'package':manifest['package'],'version':manifest['version'],'installed_verified':'--installed' in sys.argv,'package_sha256':manifest['package_sha256'],'image_or_deployment_acceptance':False}))
if __name__=='__main__':main()
