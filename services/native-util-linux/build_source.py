"""Build and inspect public util-linux only inside the disposable Linux SDK."""
from pathlib import Path
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import time


def main():
    if (sys.platform != "linux" or os.getuid() != 10001
            or os.environ.get("HAI_DISPOSABLE_NATIVE_SOURCE_SDK") != "1"
            or os.environ.get("HAI_HOSTED_SOURCE_QUALIFICATION") != "github-hosted"):
        raise ValueError("Use only the dedicated non-root hosted SDK")
    if platform.python_version() != "3.15.1":
        raise ValueError("Unexpected interpreter")
    compiler = subprocess.check_output(["gcc", "-dumpfullversion"], text=True).strip()
    if compiler != "16.2.0":
        raise ValueError("Unexpected compiler")
    spec = json.loads(Path("/inputs/sources.json").read_text())
    work, output = Path("/build"), Path("/outputs")
    work.joinpath("home").mkdir()
    work.joinpath("tmp").mkdir()
    archive = Path("/inputs") / spec["archive_name"]
    if hashlib.sha256(archive.read_bytes()).hexdigest() != spec["archive_sha256"]:
        raise ValueError("Public archive changed")
    with tarfile.open(archive, "r:xz") as tar:
        members = tar.getmembers()
        if len(members) > 30000 or sum(m.size for m in members) > 256 * 1024 * 1024:
            raise ValueError("Unexpected source archive bounds")
        tar.extractall(work, filter="data")
    source = work / spec["archive_root"]
    nsenter = (source / "sys-utils/nsenter.c").read_bytes()
    if hashlib.sha1(b"blob " + str(len(nsenter)).encode() + b"\0" + nsenter).hexdigest() != spec["expected_nsenter_git_blob"]:
        raise ValueError("Security-fix source identity differs")
    env = {"PATH": os.environ["PATH"], "HOME": "/build/home", "TMPDIR": "/build/tmp",
           "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "SOURCE_DATE_EPOCH": "0",
           "LD_LIBRARY_PATH": "/usr/local/lib64:/usr/local/lib",
           "DEB_BUILD_MAINT_OPTIONS": "hardening=+bindnow", "TZ": "UTC", "TERM": "xterm"}
    for flag in ["CFLAGS", "CPPFLAGS", "LDFLAGS"]:
        env[flag] = subprocess.check_output(["dpkg-buildflags", "--get", flag], env=env, text=True).strip()
    receipt = {"version": spec["version"], "source_sha256": spec["archive_sha256"],
               "Python": platform.python_version(), "GCC": compiler, "UID": os.getuid(),
               "configure_arguments": spec["configure_arguments"], "source_patches_applied": False,
               "vendor_case_exclusions_or_new_application_cases": False,
               "Debian_packages_built": False, "runtime_packages_installed": False,
               "complete_runtime_or_security_acceptance": False}
    # The publisher's ul fixtures contain terminal capabilities. Keep their
    # expected bytes and verify the real SDK terminfo entry instead of relying
    # on the unset-TERM fallback, which reported "trouble reading terminfo".
    terminal = subprocess.check_output(["infocmp", "-1", "xterm"], env=env)
    receipt["terminal_profile"] = "xterm"
    receipt["terminfo_entry_sha256"] = hashlib.sha256(terminal).hexdigest()
    protected = {p: hashlib.sha256(p.read_bytes()).hexdigest() for p in source.rglob("*")
                 if p.is_file() and not p.is_symlink()
                 and (p.suffix in {".c", ".h", ".sh", ".ac", ".am", ".in", ".py"}
                      or p.relative_to(source).parts[0] == "tests")}
    receipt["protected_original_source_and_case_sha256"] = {
        p.relative_to(source).as_posix(): digest for p, digest in sorted(protected.items())}
    receipt["protected_original_test_tree_files"] = sum(
        p.relative_to(source).parts[0] == "tests" for p in protected)

    def run(name, args):
        start = time.monotonic()
        with (output / (name + ".log")).open("wb") as log:
            result = subprocess.run(args, cwd=source, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=1800)
        receipt[name] = {"returncode": result.returncode, "seconds": round(time.monotonic() - start, 2), "command": args}
        if result.returncode:
            raise RuntimeError(name + " failed; inspect its retained public-source log")

    try:
        run("configure", ["./configure", *spec["configure_arguments"]])
        run("compile", ["make", "-j2"])
        run("install_prefix", ["make", "DESTDIR=/build/stage", "install"])
        prefix = work / "stage"
        files = sorted(p for p in prefix.rglob("*") if p.is_file() and not p.is_symlink())
        receipt["installed_prefix_files"] = {p.relative_to(prefix).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
        receipt["installed_prefix_symlinks"] = {p.relative_to(prefix).as_posix(): os.readlink(p) for p in prefix.rglob("*") if p.is_symlink()}
        shutil.make_archive(str(output / "source-install-prefix"), "gztar", prefix)
        try:
            run("upstream_check", ["make", "-j2", "check"])
        finally:
            # Preserve failures and automatic skips even when make exits nonzero.
            check = (output / "upstream_check.log").read_text(errors="replace")
            receipt["skip_markers"] = len(re.findall(r"\bSKIPPED\b|\bSKIP:", check))
            receipt["known_failure_markers"] = len(re.findall(r"KNOWN FAILED", check))
            receipt["failed_suite_summaries"] = re.findall(r"(\d+) tests of (\d+) FAILED", check)
            receipt["all_upstream_cases_qualified"] = False
        check = (output / "upstream_check.log").read_text(errors="replace")
        passed = re.findall(r"All (\d+) tests PASSED", check)
        receipt["upstream_reported_test_scripts"] = int(passed[-1]) if passed else None
        receipt["skip_markers"] = len(re.findall(r"\bSKIPPED\b|\bSKIP:", check))
        receipt["all_upstream_cases_qualified"] = bool(passed) and receipt["skip_markers"] == 0 and receipt["known_failure_markers"] == 0
        if not receipt["all_upstream_cases_qualified"]:
            raise RuntimeError("Full upstream-case qualification remains incomplete; skipped/unknown cases are retained")
    finally:
        changed = [p.relative_to(source).as_posix() for p, digest in protected.items()
                   if not p.is_file() or hashlib.sha256(p.read_bytes()).hexdigest() != digest]
        receipt["original_code_and_build_source_files_checked"] = len(protected)
        receipt["original_code_and_build_source_files_changed"] = changed
        # Retain the actual protected bytes, including extensionless publisher
        # cases/fixtures, for independent verification against the source archive.
        with tarfile.open(output / "protected-original-source-and-cases.tar.gz", "w:gz") as tar:
            for path in sorted(protected):
                if path.is_file() and not path.is_symlink():
                    tar.add(path, arcname=path.relative_to(source).as_posix(), recursive=False)
        # Known-failure labels suppress console diffs in the original harness.
        # Preserve its actual output/diff files without changing that harness.
        with tarfile.open(output / "upstream-case-output-and-diffs.tar.gz", "w:gz") as tar:
            for name in ["output", "diff"]:
                path = source / "tests" / name
                if path.is_dir():
                    tar.add(path, arcname=name)
        for name in ["config.log", "config.status"]:
            if (source / name).exists():
                shutil.copyfile(source / name, output / name)
        receipt["log_sha256"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in output.glob("*.log")}
        (output / "qualification.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
        if changed:
            raise RuntimeError("Original upstream code/build source changed during qualification; inspect receipt")


if __name__ == "__main__":
    main()
