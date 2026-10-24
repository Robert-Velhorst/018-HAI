"""Record fixed public binary/interpreter identities inside isolated CI images."""
from importlib.util import find_spec
from pathlib import Path, PurePosixPath
import hashlib
import json
import platform
import re
import sqlite3
import ssl
import subprocess
import sys
import sysconfig


def main():
    if sys.platform != "linux" or len(sys.argv) != 2:
        raise ValueError("Run only inside the disposable Linux runner image")
    commands = {
        "gitleaks-runner": (["/usr/local/bin/gitleaks", "version"], "8.30.1", False),
        "grype-runner": (["/usr/local/bin/grype", "version", "-o", "json"], "0.121.0", True),
        "syft-runner": (["/usr/local/bin/syft", "version", "-o", "json"], "1.55.0", True),
        "wasi-runner": (["/usr/local/bin/wasmtime", "--version"], "50.0.0", False),
        "gosec-runner": (["/usr/local/bin/gosec", "-version"], "2.30.0", False),
        "whispercpp-runner": (["/usr/local/bin/whisper-cli", "--version"], "1.9.5", False),
    }
    args, expected, json_output = commands[sys.argv[1]]
    result = subprocess.run(args, check=True, capture_output=True, text=True, timeout=10,
                            env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/tmp",
                                 "XDG_CACHE_HOME": "/tmp/cache", "GRYPE_DB_AUTO_UPDATE": "false",
                                 "GRYPE_CHECK_FOR_APP_UPDATE": "false", "SYFT_CHECK_FOR_APP_UPDATE": "false"})
    if len(result.stdout) > 65536 or len(result.stderr) > 65536:
        raise ValueError("Unexpected version response size")
    if json_output:
        actual = str(json.loads(result.stdout)["version"]).removeprefix("v")
    elif sys.argv[1] == "gitleaks-runner":
        # The publisher image embeds `git describe --tags`, including its v prefix.
        match = re.fullmatch(r"v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\s*", result.stdout)
        actual = match.group(1) if match else ""
    else:
        actual = next(iter(re.findall(r"\b\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?", result.stdout)), "")
    if actual != expected or platform.python_version() != "3.15.1":
        raise ValueError(f"{sys.argv[1]}: expected binary {expected} / Python 3.15.1; got {actual!r} / {platform.python_version()}")
    bundled = Path(sysconfig.get_path("stdlib")) / "ensurepip" / "_bundled"
    if find_spec("pip") is not None or list(bundled.glob("*.whl")):
        raise ValueError("A package installer or bootstrap wheel remains")
    extra = {}
    if sys.argv[1] == "wasi-runner":
        fixtures = Path("/review/wasi-fixtures")
        manifest_raw = (fixtures / "provenance.json").read_bytes()
        if hashlib.sha256(manifest_raw).hexdigest() != "6868998be5cb802518ec287040e4a4eb728deb624383ef40d9f85ebeb1b3498c":
            raise ValueError("Original Wasmtime case provenance changed")
        manifest = json.loads(manifest_raw)
        for item in manifest["files"]:
            relative = PurePosixPath(item["file"])
            if relative.is_absolute() or len(relative.parts) != 1:
                raise ValueError("Unexpected upstream case path")
            if hashlib.sha256((fixtures / item["file"]).read_bytes()).hexdigest() != item["sha256"]:
                raise ValueError("An original Wasmtime fixture changed")
        control = {"PATH": "/usr/local/bin:/usr/bin:/bin", "WASMTIME_WASI_PREVIEW0": "true",
                   "WASMTIME_WASI_INHERIT_ENV": "false"}
        def original_case(filename, env):
            value = subprocess.run(["timeout", "5s", "/usr/local/bin/wasmtime", "run", str(fixtures / filename)],
                                   stdin=subprocess.DEVNULL, capture_output=True, text=True,
                                   env=env, cwd="/tmp", timeout=6)
            if len(value.stdout) > 65536 or len(value.stderr) > 65536:
                raise ValueError("Unexpected original-case output size")
            return value
        baseline = original_case("hello_wasi_snapshot0.wat", {"PATH": control["PATH"]})
        if baseline.returncode == 0 or "unknown import: `wasi_unstable::proc_exit`" not in baseline.stderr:
            raise ValueError("The original Wasmtime preview0 default-failure assertion differs")
        for filename in ["hello_wasi_snapshot0.wat", "hello_wasi_snapshot1.wat"]:
            value = original_case(filename, control)
            if value.returncode != 0 or value.stdout != "Hello, world!\n":
                raise ValueError("The original Wasmtime hello-world assertion differs: " + filename)
        privacy = original_case("print_env.wat", {**control, "THIS_WILL_NOT": "show up in the output"})
        if privacy.returncode != 0 or privacy.stdout != "":
            raise ValueError("The original no-inherited-guest-environment assertion differs")
        extra["original_WASI_CLI_case_qualification"] = {
            "source_commit": manifest["upstream_commit"], "fixture_files_unchanged": True,
            "original_case_assertion_paths_executed": 4,
            "default_preview0_rejection_preserved": True,
            "fixed_environment_enables_preview0": True, "preview1_preserved": True,
            "guest_environment_empty": True, "full_upstream_or_sandbox_acceptance": False}
    if sys.argv[1] == "gosec-runner":
        go = subprocess.check_output(["/usr/local/go/bin/go", "version"], text=True, timeout=10,
                                     env={"PATH": "/usr/local/go/bin:/usr/bin:/bin", "HOME": "/tmp",
                                          "GOENV": "off", "GOTOOLCHAIN": "local"}).strip()
        if not re.fullmatch(r"go version go1\.27\.3 linux/amd64", go):
            raise ValueError("The retained Go SDK differs")
        extra["go_toolchain"] = go
    if sys.argv[1] == "whispercpp-runner":
        provenance = json.loads(Path("/usr/share/doc/hai-whisper/build-provenance.json").read_text())
        if (provenance["source_commit"] != "d1be6fde11ac6e0407606b4e42fe72d34add8037"
                or provenance["gcc"] != "16.2.0" or provenance["cmake"] != "cmake version 4.4.4"):
            raise ValueError("The reviewed Whisper source or compiler differs")
        for root, key in [(Path("/opt/hai-whisper"), "installed_prefix_files"),
                          (Path("/opt/hai-whisper-runtime/lib"), "compiler_runtime_files")]:
            for name, digest in provenance[key].items():
                relative = PurePosixPath(name)
                if relative.is_absolute() or ".." in relative.parts:
                    raise ValueError("Build provenance escapes its public installation prefix")
                candidate = root.joinpath(*relative.parts)
                if not candidate.resolve().is_relative_to(root.resolve()):
                    raise ValueError("A build output link escapes its public prefix")
                if hashlib.sha256(candidate.read_bytes()).hexdigest() != digest:
                    raise ValueError("An installed native build output differs")
        extra["native_build_provenance"] = provenance
        media = subprocess.check_output(["/usr/bin/ffmpeg", "-version"], text=True, timeout=10)
        extra["ffmpeg_version_line"] = media.splitlines()[0]
        extra["FFmpeg_latest_stable_acceptance"] = False
    print(json.dumps({"runner": sys.argv[1], "python": platform.python_version(),
                      "binary_version": actual, "openssl": ssl.OPENSSL_VERSION,
                      "sqlite": sqlite3.sqlite_version, "package_installer": "absent",
                      "bootstrap_wheels": 0, "private_data_or_provider_calls": False,
                      "full_binary_module_or_security_acceptance": False, **extra}))


if __name__ == "__main__":
    main()
