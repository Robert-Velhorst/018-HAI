"""Record and compare the existing libacl public ELF surface inside the SDK."""
from pathlib import Path
import hashlib,json,re,subprocess,sys

def snapshot():
    path=Path('/usr/lib/x86_64-linux-gnu/libacl.so.1').resolve(strict=True)
    text=subprocess.check_output(['readelf','--wide','--dyn-syms',str(path)],text=True)
    symbols={}
    for line in text.splitlines():
        fields=line.split()
        if len(fields)<8 or not fields[0].endswith(':') or fields[4] not in {'GLOBAL','WEAK'} or fields[6]=='UND':continue
        symbols[fields[7].replace('@@','@')]={'type':fields[3],'size':int(fields[2])}
    if not symbols:raise ValueError('No exported library symbols')
    dynamic=subprocess.check_output(['readelf','--wide','--dynamic',str(path)],text=True)
    soname=re.search(r'\(SONAME\).*?\[(.+?)\]',dynamic)[1]
    header=subprocess.check_output(['readelf','--file-header',str(path)],text=True)
    return {'package_version':subprocess.check_output(['dpkg-query','-W','-f=${Version}','libacl1:amd64'],text=True).strip(),'path':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'soname':soname,'machine':re.search(r'(?m)^\s*Machine:\s*(.+)$',header)[1],'class':re.search(r'(?m)^\s*Class:\s*(.+)$',header)[1],'symbols':symbols}

def main():
    mode,source=sys.argv[1:3];source=Path(source)
    if mode=='record':source.write_text(json.dumps(snapshot(),indent=2)+'\n');return
    if mode!='compare' or len(sys.argv)!=4:raise ValueError('Provide record or compare output paths')
    before=json.loads(source.read_text());after=snapshot()
    missing=sorted(set(before['symbols'])-set(after['symbols']))
    changed=[name for name,info in before['symbols'].items() if name in after['symbols'] and (info['type']!=after['symbols'][name]['type'] or info['type']=='OBJECT' and info['size']!=after['symbols'][name]['size'])]
    passed=not missing and not changed and before['soname']==after['soname']=='libacl.so.1' and before['machine']==after['machine'] and before['class']==after['class']=='ELF64'
    result={'baseline':before,'candidate':after,'missing_baseline_exports':missing,'changed_baseline_symbol_types_or_object_sizes':changed,'all_baseline_public_symbol_versions_preserved':passed,'complete_ABI_or_application_acceptance':False}
    Path(sys.argv[3]).write_text(json.dumps(result,indent=2)+'\n')
    if not passed:raise RuntimeError('Candidate does not preserve the baseline public ELF surface')
if __name__=='__main__':main()
