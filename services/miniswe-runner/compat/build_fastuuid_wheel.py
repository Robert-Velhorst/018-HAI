"""Build the explicit FastUUID candidate only in hosted disposable public SDKs."""
from pathlib import Path
import argparse
import base64
import csv
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import urllib.request
import zipfile

from prepare_fastuuid_source import prepare


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    run_id, attempt = os.environ.get('GITHUB_RUN_ID', ''), os.environ.get('GITHUB_RUN_ATTEMPT', '')
    if os.environ.get('GITHUB_ACTIONS') != 'true' or sys.platform != 'linux' or not run_id.isdigit() or not attempt.isdigit():
        raise RuntimeError('Native SDK execution is restricted to an identified hosted job')
    spec = json.loads(Path(__file__).with_name('fastuuid-port-spec.json').read_text())
    output = args.output.resolve(strict=True)
    folder = output / 'fastuuid-build'
    folder.mkdir(exist_ok=False)
    source = folder / 'source'
    prepare(source)
    exports = folder / 'exports'
    exports.mkdir()
    exports.chmod(0o777)
    tools = folder / 'tools'
    tools.mkdir()
    wheel = spec['maturin']
    raw = urllib.request.urlopen(wheel['url'], timeout=60).read()
    if hashlib.sha256(raw).hexdigest() != wheel['digests']['sha256']:
        raise ValueError('Pinned Maturin wheel differs')
    (tools / wheel['filename']).write_bytes(raw)
    owner = 'miniswe-fastuuid-' + run_id + '-' + attempt
    # Copy only public SDK prefixes into owned hosted output; mount every
    # prefix read-only during compilation. No operator toolchain is installed.
    def copy_sdk(image, mappings):
        created = subprocess.run(['docker','create','--label','io.hai.sdk-owner='+owner,
                                  image],capture_output=True,check=True,timeout=120)
        container = created.stdout.decode().strip()
        if not re.fullmatch(r'[0-9a-f]{64}',container):
            raise ValueError('Unexpected SDK container identity')
        try:
            for origin,destination in mappings:
                subprocess.run(['docker','cp',container+':'+origin,str(destination)],
                               capture_output=True,check=True,timeout=180)
        finally:
            subprocess.run(['docker','rm',container],capture_output=True,check=True,timeout=30)

    copy_sdk(spec['python_sdk'],[('/usr/local',folder/'python')])
    rust = folder/'rust'
    rust.mkdir()
    copy_sdk(spec['rust']['image'],[('/usr/local/cargo',rust/'cargo'),
                                  ('/usr/local/rustup',rust/'rustup')])
    cidfile = folder / 'owned-native-build.cid'
    command = ['docker','run','--rm','--cidfile',str(cidfile),'--label','io.hai.sdk-owner='+owner,
        '--read-only','--user','10001:10001','--cap-drop','ALL','--security-opt','no-new-privileges:true',
        '--memory','2g','--cpus','2','--pids-limit','256',
        '--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=256m,uid=10001,gid=10001,mode=0700',
        '--tmpfs','/build:rw,exec,nosuid,nodev,size=1g,uid=10001,gid=10001,mode=0700',
        '--env','HOME=/build','--env','CARGO_HOME=/build/cargo','--env','CARGO_TARGET_DIR=/build/target',
        '--env','RUSTUP_HOME=/rust/rustup','--env','RUSTUP_TOOLCHAIN=1.99.1',
        '--env','CC=/usr/local/bin/gcc','--env','CXX=/usr/local/bin/g++',
        '--env','CARGO_TARGET_X86_64_UNKNOWN_LINUX_GNU_LINKER=/usr/local/bin/gcc',
        '--env','LD_LIBRARY_PATH=/python/lib:/usr/local/lib64:/usr/local/lib',
        '--env','PATH=/python/bin:/rust/cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
        '--env','SOURCE_DATE_EPOCH=1792790400','--env','PYTHONDONTWRITEBYTECODE=1']
    for src,dst,readonly in [(source,'/source',True),(folder/'python','/python',True),(rust,'/rust',True),
                              (tools,'/tools',True),(exports,'/exports',False)]:
        command += ['--mount',f'type=bind,src={src},dst={dst}'+(',readonly' if readonly else '')]
    # No rustup bootstrap, warning suppression, ABI forward-compatibility flag,
    # application source, credentials or database is included in this SDK.
    script = '''set -eu
rustc --version | tee /exports/rustc-version.txt
cargo --version | tee /exports/cargo-version.txt
"$CC" --version > /exports/cc-version.txt
"$CXX" --version > /exports/cxx-version.txt
test "$("$CC" -dumpfullversion)" = 16.2.0
test "$("$CXX" -dumpfullversion)" = 16.2.0
python -c 'import sys; assert sys.version_info[:3] == (3,15,1)'
test "$(rustc --version | cut -d ' ' -f 2)" = 1.99.1
python -m pip install --no-index --no-deps --target /build/maturin /tools/*.whl
export PYTHONPATH=/build/maturin
cp -R /source /build/source
cd /build/source
cargo generate-lockfile
cp Cargo.lock HAI_SOURCE_PORT.json /exports/
/build/maturin/bin/maturin --version | tee /exports/maturin-version.txt
/build/maturin/bin/maturin build --locked --release --compatibility linux --interpreter /python/bin/python3.15 --out /exports
'''
    command += [spec['compiler']['sdk_image'],'sh','-c',script]
    try:
        result = subprocess.run(command,capture_output=True,check=False,timeout=720)
    except subprocess.TimeoutExpired as error:
        if cidfile.is_file():
            cid = cidfile.read_text().strip()
            if re.fullmatch(r'[0-9a-f]{64}',cid):
                label = subprocess.run(['docker','inspect','--format','{{index .Config.Labels "io.hai.sdk-owner"}}',cid],capture_output=True,text=True,timeout=10)
                if label.returncode == 0 and label.stdout.strip() == owner:
                    subprocess.run(['docker','rm','--force',cid],capture_output=True,check=True,timeout=30)
        result = subprocess.CompletedProcess(command,124,error.stdout or b'',error.stderr or b'')
    (output/'fastuuid-build.log').write_bytes(result.stdout+result.stderr)
    if result.returncode:
        print((result.stdout+result.stderr).decode(errors='replace')[-10000:])
        raise RuntimeError('Hosted FastUUID native build failed')
    wheels = list(exports.glob('fastuuid-*.whl'))
    if len(wheels) != 1:
        raise ValueError('Expected exactly one FastUUID candidate wheel')
    data = wheels[0].read_bytes()
    native = []
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        record = next(n for n in archive.namelist() if n.endswith('.dist-info/RECORD'))
        metadata = archive.read(record.removesuffix('RECORD')+'METADATA').decode()
        if '\nVersion: '+spec['local_version']+'\n' not in metadata:
            raise ValueError('FastUUID candidate version identity differs')
        info = archive.read(record.removesuffix('RECORD')+'WHEEL').decode()
        if 'Root-Is-Purelib: false' not in info or 'Tag: cp315-cp315-linux_x86_64' not in info:
            raise ValueError('FastUUID wheel must retain its honest native platform tag')
        rows = list(csv.reader(io.StringIO(archive.read(record).decode())))
        for name,checksum,size in rows:
            if not checksum:
                if name != record: raise ValueError('Unhashed wheel member')
                continue
            value = archive.read(name)
            expected = 'sha256='+base64.urlsafe_b64encode(hashlib.sha256(value).digest()).rstrip(b'=').decode()
            if checksum != expected or len(value) != int(size): raise ValueError('Wheel RECORD differs')
            if name.endswith('.so'): native.append({'path':name,'sha256':hashlib.sha256(value).hexdigest()})
    if len(native) != 1: raise ValueError('Expected one native FastUUID extension')
    destination = output/'inputs'/wheels[0].name
    destination.write_bytes(data)
    receipt = {'filename':destination.name,'sha256':hashlib.sha256(data).hexdigest(),
        'local_version':spec['local_version'],'upstream_source_sha256':spec['source_sha256'],
        'source_port_spec_sha256':hashlib.sha256(Path(__file__).with_name('fastuuid-port-spec.json').read_bytes()).hexdigest(),
        'native_members':native,'RECORD_entries_verified':len(rows),
        'cargo_lock_sha256':hashlib.sha256((exports/'Cargo.lock').read_bytes()).hexdigest(),
        'rust_sdk':spec['rust']['image'],'python_sdk':spec['python_sdk'],
        'compiler_sdk':spec['compiler']['sdk_image'],'C_and_CXX_version_asserted':'16.2.0',
        'Rust_Cargo_linker':'/usr/local/bin/gcc','SDK_prefixes_mounted_readonly':True,
        'maturin_wheel_sha256':wheel['digests']['sha256'],
        'non_root_uid':10001,'operator_sdk_installed':False,'application_cases_run':False,
        'runtime_qualified':False,'private_source_or_credentials_mounted':False}
    (output/'fastuuid-build.json').write_text(json.dumps(receipt,indent=2)+'\n')
    (output/'inputs/fastuuid-build.json').write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps(receipt))


if __name__ == '__main__':
    main()
