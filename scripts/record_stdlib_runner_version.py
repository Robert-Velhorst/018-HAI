"""Record fixed public binary/interpreter identities inside isolated CI images."""
from importlib.util import find_spec
from pathlib import Path
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
    }
    args, expected, json_output = commands[sys.argv[1]]
    result = subprocess.run(args, check=True, capture_output=True, text=True, timeout=10,
                            env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/tmp",
                                 "XDG_CACHE_HOME": "/tmp/cache", "GRYPE_DB_AUTO_UPDATE": "false",
                                 "GRYPE_CHECK_FOR_APP_UPDATE": "false", "SYFT_CHECK_FOR_APP_UPDATE": "false"})
    if len(result.stdout) > 65536 or len(result.stderr) > 65536:
        raise ValueError("Unexpected version response size")
    actual = (str(json.loads(result.stdout)["version"]).lstrip("v") if json_output
              else next(iter(re.findall(r"\b\d+\.\d+\.\d+\b", result.stdout)), ""))
    if actual != expected or platform.python_version() != "3.15.1":
        raise ValueError("The installed binary/interpreter differs from the reviewed source")
    bundled = Path(sysconfig.get_path("stdlib")) / "ensurepip" / "_bundled"
    if find_spec("pip") is not None or list(bundled.glob("*.whl")):
        raise ValueError("A package installer or bootstrap wheel remains")
    print(json.dumps({"runner": sys.argv[1], "python": platform.python_version(),
                      "binary_version": actual, "openssl": ssl.OPENSSL_VERSION,
                      "sqlite": sqlite3.sqlite_version, "package_installer": "absent",
                      "bootstrap_wheels": 0, "private_data_or_provider_calls": False,
                      "full_binary_module_or_security_acceptance": False}))


if __name__ == "__main__":
    main()
