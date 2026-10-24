"""Record the public Whisper build inside its disposable Linux SDK stage."""
from pathlib import Path
import hashlib
import json
import os
import platform
import subprocess
import sys


def output(*args):
    return subprocess.check_output(args, text=True, timeout=20).strip()


def hashes(root):
    files = sorted(p for p in root.rglob("*") if p.is_file() and not p.is_symlink())
    if not files or len(files) > 5000:
        raise ValueError("Unexpected public build output")
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}


def main():
    if sys.platform != "linux" or os.environ.get("HAI_DISPOSABLE_BUILD_SDK") != "1" or os.getuid() != 10001:
        raise ValueError("Use only the dedicated non-root disposable SDK stage")
    source = output("git", "-C", "/src/whisper.cpp", "rev-parse", "HEAD")
    compiler = output("g++", "-dumpfullversion")
    cmake = output("cmake", "--version").splitlines()[0]
    if (source != "d1be6fde11ac6e0407606b4e42fe72d34add8037" or compiler != "16.2.0"
            or cmake != "cmake version 4.4.4" or platform.python_version() != "3.15.1"):
        raise ValueError("The public source or build SDK differs")
    receipt = {"source_commit": source, "release": "v1.9.5", "python": platform.python_version(),
               "gcc": compiler, "cmake": cmake, "build_uid": os.getuid(),
               "installed_prefix_files": hashes(Path("/out/whisper")),
               "compiler_runtime_files": hashes(Path("/out/runtime-libs")),
               "real_model_audio_or_provider_acceptance": False}
    Path("/out/build-provenance.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
