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
    actual = (str(json.loads(result.stdout)["version"]).lstrip("v") if json_output
              else next(iter(re.findall(r"\b\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?", result.stdout)), ""))
    if actual != expected or platform.python_version() != "3.15.1":
        raise ValueError("The installed binary/interpreter differs from the reviewed source")
    bundled = Path(sysconfig.get_path("stdlib")) / "ensurepip" / "_bundled"
    if find_spec("pip") is not None or list(bundled.glob("*.whl")):
        raise ValueError("A package installer or bootstrap wheel remains")
    extra = {}
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
