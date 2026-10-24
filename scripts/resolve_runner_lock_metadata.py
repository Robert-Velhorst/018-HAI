"""Resolve public runner dependency metadata in a restricted disposable SDK.

This does not install or execute the HAI application. Public package build
backends may execute inside the container to supply missing source metadata.
"""
from pathlib import Path
import argparse
import hashlib
import io
import json
import os
import subprocess
import sys
import urllib.request
import zipfile

SDKS = {
    "garak-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "evidently-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runner", required=True, choices=SDKS)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    version, sdk = SDKS[args.runner]
    root = Path(__file__).resolve().parents[1]
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    for name in ["inputs", "outputs", "cache", "tools"]:
        (output / name).mkdir(exist_ok=True)
    # Linux container UID 10001 can write only these disposable public paths.
    for name in ["outputs", "cache"]:
        (output / name).chmod(0o777)
    input_filename = "requirements-port.in" if args.runner == "evidently-runner" else "requirements.in"
    source = (root / "services" / args.runner / input_filename).read_bytes()
    (output / "inputs/requirements.in").write_bytes(source)
    port_build = None
    if args.runner == "evidently-runner":
        build = subprocess.run([sys.executable,
            str(root/"services/evidently-runner/compat/build_port_wheel.py"),
            "--output-directory", str(output/"inputs")], capture_output=True, check=False, timeout=120)
        (output/"port-build.log").write_bytes(build.stdout + build.stderr)
        if build.returncode:
            print((build.stdout + build.stderr).decode("utf-8", errors="replace")[-12000:])
            raise SystemExit(build.returncode)
        port_build = json.loads(build.stdout)
        if port_build["sha256"] != "2db131ed9ddfc1dc15a25635c8a6e58d51e608fe7459c504f091c5bd2168bd52":
            raise ValueError("Dynaconf port wheel differs from the reviewed static build")
        (output/"port-build.json").write_text(json.dumps(port_build, indent=2)+"\n", encoding="utf-8")
    metadata_raw = urllib.request.urlopen("https://pypi.org/pypi/uv/0.13.5/json", timeout=40).read()
    metadata = json.loads(metadata_raw)
    wheels = [f for f in metadata["urls"] if f["packagetype"] == "bdist_wheel" and "manylinux_2_17_x86_64" in f["filename"]]
    assert len(wheels) == 1
    wheel = wheels[0]
    assert wheel["url"].startswith("https://files.pythonhosted.org/")
    raw = urllib.request.urlopen(wheel["url"], timeout=60).read()
    assert len(raw) < 32 * 1024 * 1024
    assert hashlib.sha256(raw).hexdigest() == wheel["digests"]["sha256"]
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        names = [n for n in archive.namelist() if n.endswith("/uv")]
        assert len(names) == 1
        executable = archive.read(names[0])
    (output / "tools/uv").write_bytes(executable)
    command = ["docker", "run", "--rm", "--read-only", "--user", "10001:10001",
               "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
               "--memory", "1g", "--cpus", "1", "--pids-limit", "128",
               "--tmpfs", "/tmp:rw,nosuid,nodev,size=768m,uid=10001,gid=10001,mode=0700",
               "--tmpfs", "/sdk-tools:rw,exec,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=0700",
               "--env", "HOME=/tmp", "--env", "UV_CACHE_DIR=/cache",
               "--env", "UV_PYTHON_DOWNLOADS=never", "--env", "UV_CONCURRENT_BUILDS=1",
               "--env", "UV_CONCURRENT_DOWNLOADS=2", "--env", "UV_CONCURRENT_INSTALLS=1"]
    for name, destination, readonly in [("inputs", "/inputs", True), ("outputs", "/outputs", False), ("cache", "/cache", False), ("tools", "/tools", True)]:
        command += ["--mount", f"type=bind,src={output / name},dst={destination}" + (",readonly" if readonly else "")]
    command += ["--workdir", "/inputs", sdk, "sh", "-c",
                'cp /tools/uv /sdk-tools/uv && chmod 700 /sdk-tools/uv && exec /sdk-tools/uv pip compile requirements.in --find-links /inputs --upgrade --generate-hashes --no-python-downloads --python-version "$1" --python-platform x86_64-unknown-linux-gnu --emit-index-url --only-binary numpy --only-binary scipy --only-binary pandas --only-binary pyarrow --only-binary tokenizers --output-file /outputs/requirements.lock',
                "public-metadata-resolution", version]
    process = subprocess.run(command, capture_output=True, timeout=600, check=False)
    log = process.stdout + process.stderr
    (output / "resolution.log").write_bytes(log)
    lock = output / "outputs/requirements.lock"
    receipt = {"runner": args.runner, "python": version, "sdk": sdk,
               "returncode": process.returncode, "input_filename": input_filename, "local_port_wheel": port_build, "input_sha256": hashlib.sha256(source).hexdigest(),
               "uv_wheel_sha256": hashlib.sha256(raw).hexdigest(),
               "uv_binary_sha256": hashlib.sha256(executable).hexdigest(),
               "log_sha256": hashlib.sha256(log).hexdigest(),
               "lock_sha256": hashlib.sha256(lock.read_bytes()).hexdigest() if lock.exists() else None,
               "source_revision": os.environ.get("GITHUB_SHA"),
               "public_build_backend_metadata_execution_possible": True,
               "private_application_source_or_credentials_mounted": False,
               "application_tests_run": False, "host_sdk_installed": False}
    (output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(receipt))
    if process.returncode:
        print(log.decode("utf-8", errors="replace")[-12000:])
        raise SystemExit(process.returncode)


if __name__ == "__main__":
    main()
