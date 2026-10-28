"""Explicit dependency source port; preserve publisher code and test commands."""
import argparse
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tomllib
import urllib.request
import zipfile

COMMIT = "d6cd78ac44da4623ff07c73310dfca474c212068"
WHEEL_URL = "https://files.pythonhosted.org/packages/d7/34/63e1ac5d5a2727a78ed6c231d40f3e8a153b9e82d8cc30e8236f264af41e/deepeval-4.3.2-py3-none-any.whl"
WHEEL_SHA = "e0ea88a7a97f453b7b2e62550f5b2881cdc3c8397f93f985a6a161fd86e6d843"
OWNER = Path(__file__).resolve().parents[2]
SDK = OWNER / "vendor-deepeval"
EVIDENCE = OWNER / "deepeval-sdk-port-evidence"
EVIDENCE.mkdir(exist_ok=True)
MODE = argparse.ArgumentParser()
MODE.add_argument("mode", choices=["prepare", "install", "qualify", "test", "audit"])


def tracked():
    names = subprocess.check_output(["git", "-C", str(SDK), "ls-files", "-z"]).split(b"\0")
    return {n.decode(): hashlib.sha256((SDK / n.decode()).read_bytes()).hexdigest()
            for n in names if n and (SDK / n.decode()).is_file()}


def command(text, label):
    # The exact publisher command body is passed intact to the same shell semantics.
    with (EVIDENCE / (label + ".log")).open("wb") as log:
        result = subprocess.run(["bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", text],
                                cwd=SDK, stdout=log, stderr=subprocess.STDOUT, check=False)
    print(json.dumps({"command": label, "sha256": hashlib.sha256(text.encode()).hexdigest(),
                      "exit_code": result.returncode}), flush=True)
    assert result.returncode == 0, label + " failed; original output retained"


def publisher_command(step):
    lines = (SDK / ".github/workflows/test_core.yml").read_text().splitlines(keepends=True)
    start = next(i for i, line in enumerate(lines) if line.strip() == "- name: " + step)
    stop = next((i for i in range(start + 1, len(lines)) if lines[i].startswith("      - ")), len(lines))
    run = next(i for i in range(start, stop) if lines[i].lstrip().startswith("run:"))
    value = lines[run].strip()[4:].strip()
    if value != "|":
        return value + "\n"
    first = next(i for i in range(run + 1, stop) if lines[i].strip())
    indent = len(lines[first]) - len(lines[first].lstrip())
    end = next((i for i in range(first, stop) if lines[i].strip()
                and len(lines[i]) - len(lines[i].lstrip()) < indent), stop)
    content = lines[run + 1:end]
    while content and not content[-1].strip():
        content.pop()
    return "".join(line[indent:] if line.strip() else "\n" for line in content)


def prepare():
    assert subprocess.check_output(["git", "-C", str(SDK), "rev-parse", "HEAD"]).decode().strip() == COMMIT
    before = tracked()
    archive = EVIDENCE / "publisher-deepeval-4.3.2.whl"
    with urllib.request.urlopen(WHEEL_URL, timeout=60) as response:
        raw = response.read(32 * 1024 * 1024 + 1)
    assert len(raw) <= 32 * 1024 * 1024 and hashlib.sha256(raw).hexdigest() == WHEEL_SHA
    archive.write_bytes(raw)
    compared = {}
    with zipfile.ZipFile(archive) as wheel:
        for name in wheel.namelist():
            assert not name.startswith("/") and ".." not in Path(name).parts
            if name.startswith("deepeval/") and not name.endswith("/"):
                content = wheel.read(name)
                assert (SDK / name).read_bytes() == content, (name, "publisher/commit source mismatch")
                compared[name] = hashlib.sha256(content).hexdigest()
    assert compared
    path = SDK / "pyproject.toml"
    original = path.read_text()
    assert tomllib.loads(original)["tool"]["poetry"]["version"] == "4.3.2"
    replacements = {
        'click = ">=8.0.0,<8.4.0"': 'click = ">=8.0.0,<9.0.0"',
        'rich = ">=13.6.0, <15.0.0"': 'rich = ">=13.6.0, <16.0.0"',
        'tabulate = "^0.9.0"': 'tabulate = ">=0.9.0,<0.11.0"',
    }
    candidate = original
    for old, new in replacements.items():
        assert candidate.count(old) == 1
        candidate = candidate.replace(old, new, 1)
    assert tomllib.loads(candidate)["tool"]["poetry"]["version"] == "4.3.2"
    path.write_bytes(candidate.encode())
    commands = {step: publisher_command(step) for step in [
        "Install dependencies", "Install project", "Install dev dependencies",
        "Run tests (no secrets)", "Run dev tests (no secrets)"]}
    record = {"upstream_commit": COMMIT, "publisher_wheel_sha256": WHEEL_SHA,
              "packaged_SDK_files_compared": compared, "original_source": before,
              "declaration_changes": replacements, "publisher_commands": commands,
              "source_port_not_unmodified_publisher_release": True,
              "version_not_relabelled": True, "full_vendor_acceptance": False}
    (EVIDENCE / "source-port-identity.json").write_bytes((json.dumps(record, indent=2) + "\n").encode())
    print(json.dumps({"publisher_payloads_matched": len(compared), "declarations_changed": 3,
                      "original_case_and_runtime_source_edits": False}), flush=True)


def qualify():
    wheels = list((SDK / "dist").glob("deepeval-4.3.2-*.whl"))
    assert len(wheels) == 1
    wheel = wheels[0].resolve()
    sha = hashlib.sha256(wheel.read_bytes()).hexdigest()
    text = (OWNER / "services/deepeval-runner/requirements.lock").read_text()
    blocks = re.split(r"(?m)(?=^[\w.-]+==)", text)
    replaced = 0
    result = []
    for block in blocks:
        if block.startswith("deepeval==4.3.2 "):
            block = "deepeval @ " + wheel.as_uri() + " \\\n    --hash=sha256:" + sha + "\n\n"
            replaced += 1
        result.append(block)
    assert replaced == 1
    port_lock = EVIDENCE / "complete-source-port-runtime.lock"
    port_lock.write_bytes("".join(result).encode())
    python = SDK / ".venv/bin/python"
    assert python.is_file()
    calls = [
        [str(python), "-m", "pip", "install", "--no-cache-dir", "--only-binary", ":all:", "--require-hashes", "-r", str(OWNER / "services/deepeval-runner/requirements-build.ubuntu.lock")],
        [str(python), "-m", "pip", "install", "--no-cache-dir", "--no-build-isolation", "--require-hashes", "-r", str(port_lock)],
        [str(python), "-m", "pip", "check"],
    ]
    for index, call in enumerate(calls):
        with (EVIDENCE / ("complete-install-" + str(index) + ".log")).open("wb") as log:
            subprocess.run(call, cwd=SDK, stdout=log, stderr=subprocess.STDOUT, check=True)
    # All selected versions, including the explicitly approved exception, must be present.
    expected = dict(re.findall(r"(?m)^([\w.-]+)==([^\s;\\]+)", text))
    expected.update(dict(re.findall(r"(?m)^([\w.-]+)==([^\s;\\]+)",
        (OWNER / "services/deepeval-runner/requirements-build.ubuntu.lock").read_text())))
    actual = json.loads(subprocess.check_output([str(python), "-c",
        "import importlib.metadata,json;print(json.dumps({d.metadata['Name'].lower().replace('_','-'):d.version for d in importlib.metadata.distributions()}))"]))
    assert all(actual[n.lower().replace("_", "-")] == v for n, v in expected.items())
    invalid = {n: v for n, v in actual.items() if not re.fullmatch(r"\d+(?:\.\d+)*(?:\.post\d+)?", v)
               and not (n == "opentelemetry-semantic-conventions" and v == "0.66b1")}
    (EVIDENCE / "installed-source-port.json").write_bytes((json.dumps({"port_wheel_sha256": sha,
        "all_original_runtime_projects_present": len(expected), "installed": actual,
        "unapproved_versions": invalid, "full_vendor_acceptance": False}, indent=2) + "\n").encode())
    assert not invalid, "No other prerelease or local version exception is approved"


def audit():
    record = json.loads((EVIDENCE / "source-port-identity.json").read_bytes())
    current = tracked()
    assert current.keys() == record["original_source"].keys()
    changed = [name for name in current if current[name] != record["original_source"][name]]
    assert set(changed) <= {"pyproject.toml", "poetry.lock"}, changed
    (EVIDENCE / "source-preservation-after.json").write_bytes((json.dumps({
        "changed_publisher_files": changed, "all_original_SDK_cases_helpers_and_runtime_code_unchanged": True,
        "original_publisher_commands": record["publisher_commands"], "full_vendor_acceptance": False}, indent=2) + "\n").encode())


if __name__ == "__main__":
    mode = MODE.parse_args().mode
    if mode == "prepare":
        prepare()
    elif mode == "qualify":
        qualify()
    elif mode == "audit":
        audit()
    else:
        record = json.loads((EVIDENCE / "source-port-identity.json").read_bytes())
        steps = ["Install dependencies", "Install project", "Install dev dependencies"] if mode == "install" else ["Run tests (no secrets)", "Run dev tests (no secrets)"]
        for index, step in enumerate(steps):
            assert publisher_command(step) == record["publisher_commands"][step]
            command(record["publisher_commands"][step], mode + "-" + str(index))
