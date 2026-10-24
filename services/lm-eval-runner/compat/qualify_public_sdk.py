"""Install/import public candidates only in owned hosted disposable SDKs."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--sdk", required=True)
    args = parser.parse_args()
    expected_sdk = "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"
    if os.environ.get("GITHUB_ACTIONS") != "true" or sys.platform != "linux" or args.sdk != expected_sdk:
        raise RuntimeError("Requires the source-pinned hosted public SDK; local SDK execution is refused")
    run_id, attempt = os.environ.get("GITHUB_RUN_ID", ""), os.environ.get("GITHUB_RUN_ATTEMPT", "")
    if not run_id.isdigit() or not attempt.isdigit():
        raise RuntimeError("Missing hosted job identity")
    owner = "lmeval-current-sdk-" + run_id + "-" + attempt
    output = args.output.resolve(strict=True)
    inputs = output / "inputs"
    site = output / "site-packages"
    proof = output / "qualification"
    for path in [site, proof]:
        path.mkdir(exist_ok=False)
        path.chmod(0o777)  # Public-only disposable outputs for UID 10001.
    helper = Path(__file__).with_name("verify_sdk_import.py").read_bytes()
    (inputs / "verify_sdk_import.py").write_bytes(helper)
    expected={re.sub(r"[-_.]+","-",name.lower()):version for name,version in
              re.findall(r"(?m)^([A-Za-z0-9_.-]+)==([^\s;\\]+)",(output/"outputs/requirements.lock").read_text(encoding="utf-8"))}
    if len(expected)!=70:raise ValueError("Unexpected current LM candidate graph size")
    (inputs/"expected-distributions.json").write_text(json.dumps(expected,sort_keys=True)+"\n",encoding="utf-8")
    for filename in ["port-spec.json","python-parent-port-specs.json"]:
        (inputs/filename).write_bytes(Path(__file__).with_name(filename).read_bytes())

    def run_owned(command, phase, seconds):
        cidfile = output / ("owned-sdk-" + phase + ".cid")
        command = command[:3] + ["--cidfile", str(cidfile), "--label", "io.hai.sdk-owner=" + owner] + command[3:]
        try:
            return subprocess.run(command, capture_output=True, timeout=seconds, check=False)
        except subprocess.TimeoutExpired as error:
            # Stop only this helper's explicitly identified disposable container.
            if cidfile.is_file():
                container = cidfile.read_text().strip()
                if re.fullmatch(r"[0-9a-f]{64}", container):
                    label = subprocess.run(["docker", "inspect", "--format",
                        '{{index .Config.Labels "io.hai.sdk-owner"}}', container], capture_output=True, text=True, timeout=10)
                    if label.returncode == 0 and label.stdout.strip() == owner:
                        subprocess.run(["docker", "rm", "--force", container], capture_output=True, timeout=20, check=True)
            return subprocess.CompletedProcess(command, 124, error.stdout or b"", (error.stderr or b"") + b"\nOwned public SDK phase timed out\n")
    common = ["docker", "run", "--rm", "--read-only", "--user", "10001:10001",
              "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
              "--memory", "1g", "--cpus", "1", "--pids-limit", "128",
              "--tmpfs", "/tmp:rw,nosuid,nodev,size=768m,uid=10001,gid=10001,mode=0700",
              "--env", "HOME=/tmp", "--env", "PYTHONDONTWRITEBYTECODE=1"]
    install = common + ["--env", "UV_PYTHON_DOWNLOADS=never", "--env", "UV_CACHE_DIR=/cache",
              "--tmpfs", "/sdk-tools:rw,exec,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=0700"]
    for src, dst, readonly in [(inputs,"/inputs",True),(output/"outputs","/outputs",True),
                               (output/"tools","/tools",True),(output/"cache","/cache",False),(site,"/installed",False)]:
        install += ["--mount",f"type=bind,src={src},dst={dst}" + (",readonly" if readonly else "")]
    install += [args.sdk,"sh","-c",
        "cp /tools/uv /sdk-tools/uv && chmod 700 /sdk-tools/uv && exec /sdk-tools/uv pip install --no-python-downloads --require-hashes --find-links /inputs --target /installed -r /outputs/requirements.lock"]
    result = run_owned(install, "install", 360)
    (proof/"sdk-install.log").write_bytes(result.stdout+result.stderr)
    import_returncode = None
    if result.returncode == 0:
        check = common + ["--network","none","--env","HAI_DISPOSABLE_PUBLIC_SDK=1",
                  "--env","HF_HUB_OFFLINE=1","--env","HF_HUB_DISABLE_TELEMETRY=1"]
        for src,dst,readonly in [(inputs,"/inputs",True),(site,"/installed",True),(proof,"/qualification",False)]:
            check += ["--mount",f"type=bind,src={src},dst={dst}" + (",readonly" if readonly else "")]
        check += [args.sdk,"python","-I","/inputs/verify_sdk_import.py","--site-packages","/installed",
                  "--receipt","/qualification/sdk-import.json"]
        imported = run_owned(check, "import", 180)
        import_returncode = imported.returncode
        (proof/"sdk-import.log").write_bytes(imported.stdout+imported.stderr)
    qualified = result.returncode == 0 and import_returncode == 0 and (proof/"sdk-import.json").is_file()
    receipt = {"installation_returncode":result.returncode,"import_returncode":import_returncode,
               "qualified":qualified,"verification_helper_sha256":hashlib.sha256(helper).hexdigest(),
               "SDK_import_network":"none","installed_packages_mounted_readonly_for_import":True,
               "non_root_uid":10001,"private_application_source_or_credentials_mounted":False,
               "operator_sdk_installed":False,"application_cases_run":False}
    (proof/"qualification.json").write_text(json.dumps(receipt,indent=2)+"\n",encoding="utf-8")
    print(json.dumps(receipt))
    if not qualified:
        for path in [proof/"sdk-install.log",proof/"sdk-import.log"]:
            if path.exists():
                print(path.read_text(encoding="utf-8",errors="replace")[-6000:])
        raise SystemExit(1)


if __name__ == "__main__":
    main()
