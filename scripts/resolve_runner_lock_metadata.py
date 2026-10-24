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
    "miniswe-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "lm-eval-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "crewai-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "ortools-runtime": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "ortools-build": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "agent-framework-runner": ("3.15.1", "python:3.15.1-slim-bookworm@sha256:575f8bc12ec26542e0678bc6b60e385218248019d433157ec327264a78c3a853"),
    "guardrails-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "pydantic-ai-runner": ("3.15.1", "python:3.15.1-slim-bookworm@sha256:575f8bc12ec26542e0678bc6b60e385218248019d433157ec327264a78c3a853"),
    "garak-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
    "evidently-runner": ("3.15.1", "python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51"),
}


INPUTS = {
    "ortools-runtime": ("ortools-solver", "requirements.runtime-port.in"),
    "ortools-build": ("ortools-solver", "requirements.build-port.in"),
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
    service, input_filename = INPUTS.get(args.runner, (args.runner,
        "requirements-port.in" if args.runner in {"evidently-runner", "guardrails-runner", "lm-eval-runner", "miniswe-runner"} else "requirements.in"))
    input_path = Path("services") / service / input_filename
    source = (root / input_path).read_bytes()
    (output / "inputs/requirements.in").write_bytes(source)
    port_build = None
    evidently_port_build = None
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
        build = subprocess.run([sys.executable,
            str(root/"services/evidently-runner/compat/build_evidently_wheel.py"),
            "--output-directory", str(output/"inputs")], capture_output=True, check=False, timeout=120)
        (output/"evidently-port-build.log").write_bytes(build.stdout + build.stderr)
        if build.returncode:
            print((build.stdout + build.stderr).decode("utf-8", errors="replace")[-12000:])
            raise SystemExit(build.returncode)
        evidently_port_build = json.loads(build.stdout)
        if evidently_port_build["sha256"] != "e604b98d65f00e043ffdf1a2c33a059050f131848f79c04b17d0a2d38bdbc72c":
            raise ValueError("Evidently port wheel differs from the reviewed static build")
        (output/"evidently-port-build.json").write_text(json.dumps(evidently_port_build, indent=2)+"\n", encoding="utf-8")
    if args.runner == "guardrails-runner":
        build = subprocess.run([sys.executable,
            str(root/"services/guardrails-runner/compat/build_port_wheel.py"),
            "--output-directory", str(output/"inputs")], capture_output=True, check=False, timeout=120)
        (output/"port-build.log").write_bytes(build.stdout + build.stderr)
        if build.returncode:
            print((build.stdout + build.stderr).decode("utf-8", errors="replace")[-12000:])
            raise SystemExit(build.returncode)
        port_build = json.loads(build.stdout)
        if port_build["sha256"] != "c16222e9b42be2b593e19e97649058e92ad5df2fa3b47cb4f912525a1ad86fa4":
            raise ValueError("Guardrails port wheel differs from the reviewed static build")
        (output/"port-build.json").write_text(json.dumps(port_build, indent=2)+"\n", encoding="utf-8")
    if args.runner == "lm-eval-runner":
        build = subprocess.run([sys.executable,
            str(root/"services/lm-eval-runner/compat/build_port_wheel.py"),
            "--output-directory", str(output/"inputs")], capture_output=True, check=False, timeout=120)
        (output/"port-build.log").write_bytes(build.stdout + build.stderr)
        if build.returncode:
            raise RuntimeError("LM Eval metadata port construction failed")
        port_build = json.loads(build.stdout)
        if port_build["sha256"] != "0cc1a5307a648a8f858f3181e5df8c23b07f6af6681e33e8b3d102dd3e8ddf7b":
            raise ValueError("LM Eval metadata port differs from the reviewed static build")
        (output/"port-build.json").write_text(json.dumps(port_build, indent=2)+"\n", encoding="utf-8")
    if args.runner == "miniswe-runner":
        build = subprocess.run([sys.executable,
            str(root/"services/miniswe-runner/compat/build_port_wheel.py"),
            "--output-directory", str(output/"inputs")], capture_output=True, check=False, timeout=120)
        (output/"port-build.log").write_bytes(build.stdout+build.stderr)
        if build.returncode:
            raise RuntimeError("LiteLLM current-SDK candidate construction failed")
        port_build=json.loads(build.stdout)
        if port_build["sha256"] != "4522bd0c2914a03df1d02ae29c56087d65d3b43bfbc941cef2bfd98587e23c36":
            raise ValueError("LiteLLM candidate differs from the reviewed static build")
        (output/"port-build.json").write_text(json.dumps(port_build,indent=2)+"\n",encoding="utf-8")
        native = subprocess.run([sys.executable,
            str(root/"services/miniswe-runner/compat/build_fastuuid_wheel.py"),
            "--output",str(output)],capture_output=True,check=False,timeout=1200)
        (output/"fastuuid-preparation.log").write_bytes(native.stdout+native.stderr)
        if native.returncode:
            print((native.stdout+native.stderr).decode("utf-8",errors="replace")[-12000:])
            raise RuntimeError("Source-bound FastUUID native candidate construction failed")
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
    receipt = {"runner": args.runner, "input_path": input_path.as_posix(), "python": version, "sdk": sdk,
               "returncode": process.returncode, "input_filename": input_filename, "local_port_wheel": port_build,
               "local_evidently_wheel": evidently_port_build, "input_sha256": hashlib.sha256(source).hexdigest(),
               "uv_wheel_sha256": hashlib.sha256(raw).hexdigest(),
               "uv_binary_sha256": hashlib.sha256(executable).hexdigest(),
               "log_sha256": hashlib.sha256(log).hexdigest(),
               "lock_sha256": hashlib.sha256(lock.read_bytes()).hexdigest() if lock.exists() else None,
               "source_revision": os.environ.get("GITHUB_SHA"),
               "public_build_backend_metadata_execution_possible": True,
               "private_application_source_or_credentials_mounted": False,
               "application_tests_run": False, "host_sdk_installed": False}
    if args.runner == "miniswe-runner" and process.returncode == 0:
        qualified = subprocess.run([sys.executable,
            str(root/"services/miniswe-runner/compat/qualify_public_sdk.py"),
            "--output",str(output),"--sdk",sdk],capture_output=True,check=False,timeout=600)
        (output/"public-sdk-qualification.log").write_bytes(qualified.stdout+qualified.stderr)
        receipt["public_sdk_qualification_returncode"]=qualified.returncode
        receipt["public_sdk_import_qualified"]=qualified.returncode==0
        receipt["public_vendor_sdk_code_executed_in_disposable_container"]=True if qualified.returncode==0 else None
        if qualified.returncode:
            (output/"receipt.json").write_text(json.dumps(receipt,indent=2)+"\n",encoding="utf-8")
            print((qualified.stdout+qualified.stderr).decode("utf-8",errors="replace")[-14000:])
            raise SystemExit(qualified.returncode)
    (output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(receipt))
    if process.returncode:
        print(log.decode("utf-8", errors="replace")[-12000:])
        raise SystemExit(process.returncode)


if __name__ == "__main__":
    main()
