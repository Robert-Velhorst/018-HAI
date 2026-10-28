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
MODE.add_argument("mode", choices=["prepare", "plan", "install", "qualify", "test", "audit"])


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


def current_python_profile(original, selected=None, group_profile=None):
    """Preserve original constraints outside 3.15; pin current inputs inside it."""
    document = tomllib.loads(original)
    dependencies = document["tool"]["poetry"]["dependencies"]
    if selected is None:
        selected = {}
        for name in ["requirements.lock", "requirements-build.ubuntu.lock"]:
            text = (OWNER / "services/deepeval-runner" / name).read_text()
            for project, version in re.findall(r"(?m)^([\w.-]+)==([^\s;\\]+)", text):
                selected[re.sub(r"[-_.]+", "-", project.lower())] = version
    if group_profile is None:
        group_profile = json.loads((OWNER / ".github/qualifications/deepeval-sdk-group-current-profile.json").read_bytes())
    assert group_profile["upstream_commit"] == COMMIT

    def render(value):
        if isinstance(value, dict):
            return "{ " + ", ".join(key + " = " + render(item) for key, item in value.items()) + " }"
        if isinstance(value, list):
            return "[" + ", ".join(render(item) for item in value) + "]"
        return json.dumps(value)

    start = original.index("[tool.poetry.dependencies]")
    end = original.find("\n[", start + 1)
    if end == -1:
        end = len(original)
    section = original[start:end]
    expected = json.loads(json.dumps(document))
    changes = {}
    for name, value in dependencies.items():
        normalized = re.sub(r"[-_.]+", "-", name.lower())
        if name == "python" or normalized not in selected:
            continue
        if isinstance(value, str):
            old = [{"version": value, "python": "<3.15 || >=3.16"}]
            modern = {"version": selected[normalized], "python": ">=3.15,<3.16"}
        else:
            # The pinned publisher source has one existing multi-Python dependency.
            # Keep both original branches, excluding only the explicitly ported minor.
            assert name == "posthog" and isinstance(value, list)
            assert [row["python"] for row in value] == ["<3.10", ">=3.10"]
            old = [dict(row) for row in value]
            old[1]["python"] = ">=3.10,<3.15 || >=3.16"
            modern = {**value[1], "version": selected[normalized], "python": ">=3.15,<3.16"}
        replacement = old + [modern]
        expression = re.search(r"(?m)^" + re.escape(name) + r"\s*=\s*", section)
        assert expression
        value_start = expression.end()
        remaining = section[value_start:].splitlines(keepends=True)
        chunk = ""
        for line in remaining:
            chunk += line
            try:
                parsed = tomllib.loads("value = " + chunk)["value"]
            except tomllib.TOMLDecodeError:
                continue
            assert parsed == value
            break
        else:
            raise AssertionError("Cannot identify exact original dependency declaration")
        section = section[:value_start] + render(replacement) + "\n" + section[value_start + len(chunk):]
        expected["tool"]["poetry"]["dependencies"][name] = replacement
        changes[name] = {"original": value, "source_port": replacement}
    candidate = original[:start] + section + original[end:]
    unsupported = []
    for group, original_rows in group_profile["original_groups"].items():
        assert document["tool"]["poetry"]["group"][group]["dependencies"] == original_rows
        heading = "[tool.poetry.group." + group + ".dependencies]"
        start = candidate.index(heading)
        end = candidate.find("\n[", start + 1)
        if end == -1:
            end = len(candidate)
        section = candidate[start:end]
        for name, value in original_rows.items():
            normalized = re.sub(r"[-_.]+", "-", name.lower())
            release = group_profile["releases"][normalized]
            version = release["version"]
            assert re.fullmatch(r"\d+(?:\.\d+)*(?:\.post\d+)?", version)
            if normalized in selected:
                assert selected[normalized] == version, "Do not downgrade a current runtime input"
            if not supports_python315(release["requires_python"]):
                unsupported.append({"group": group, "name": name, "version": version,
                                    "requires_python": release["requires_python"],
                                    "original_declaration_retained": value})
                continue
            if isinstance(value, str):
                base = {"version": value}
            else:
                assert isinstance(value, dict)
                base = dict(value)
                assert "python" not in base.get("markers", ""), "Review mixed Python/platform markers explicitly"
            old = dict(base)
            original_scope = base.get("python")
            if not original_scope:
                old["python"] = "<3.15 || >=3.16"
            elif supports_python315(original_scope):
                old["python"] = original_scope + ",<3.15 || " + original_scope + ",>=3.16"
            modern = {**base, "version": version, "python": ">=3.15,<3.16"}
            replacement = [old, modern]
            expression = re.search(r"(?m)^" + re.escape(name) + r"\s*=\s*", section)
            assert expression
            value_start = expression.end()
            chunk = ""
            for line in section[value_start:].splitlines(keepends=True):
                chunk += line
                try:
                    parsed = tomllib.loads("value = " + chunk)["value"]
                except tomllib.TOMLDecodeError:
                    continue
                assert parsed == value
                break
            else:
                raise AssertionError("Cannot identify exact original group declaration")
            section = section[:value_start] + render(replacement) + "\n" + section[value_start + len(chunk):]
            expected["tool"]["poetry"]["group"][group]["dependencies"][name] = replacement
            changes["group:" + group + ":" + name] = {"original": value, "source_port": replacement}
        candidate = candidate[:start] + section + candidate[end:]
    assert tomllib.loads(candidate) == expected
    assert expected["tool"]["poetry"]["version"] == "4.3.2"
    assert expected["tool"]["poetry"]["dependencies"]["python"] == dependencies["python"]
    assert changes["pydantic"]["source_port"][-1]["version"] == selected["pydantic"]
    return candidate, changes, unsupported


def supports_python315(specification):
    if not specification:
        return True
    actual = (3, 15, 2)
    for alternative in specification.split("||"):
        accepted = True
        for clause in alternative.split(","):
            match = re.fullmatch(r"\s*(>=|<=|==|!=|>|<)\s*(\d+(?:\.\d+)*)\s*", clause)
            assert match, "Review unsupported Python constraint syntax explicitly"
            value = tuple(map(int, match[2].split(".")))
            value += (0,) * (3 - len(value))
            comparisons = {">=": actual >= value, "<=": actual <= value, "==": actual == value,
                           "!=": actual != value, ">": actual > value, "<": actual < value}
            accepted = accepted and comparisons[match[1]]
        if accepted:
            return True
    return False


def prepare():
    assert not os.environ.get("PYO3_USE_ABI3_FORWARD_COMPATIBILITY"), "No native ABI guard override is permitted"
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
    candidate, replacements, unsupported = current_python_profile(original)
    assert tomllib.loads(candidate)["tool"]["poetry"]["version"] == "4.3.2"
    path.write_bytes(candidate.encode())
    commands = {step: publisher_command(step) for step in [
        "Install dependencies", "Install project", "Install dev dependencies",
        "Run tests (no secrets)", "Run dev tests (no secrets)"]}
    record = {"upstream_commit": COMMIT, "publisher_wheel_sha256": WHEEL_SHA,
              "packaged_SDK_files_compared": compared, "original_source": before,
              "declaration_changes": replacements, "publisher_commands": commands,
              "original_python_constraints_preserved_outside_python315": True,
              "original_dependency_groups_preserved": True,
              "public_dependencies_still_excluding_python315": unsupported,
              "source_port_not_unmodified_publisher_release": True,
              "version_not_relabelled": True, "full_vendor_acceptance": False}
    (EVIDENCE / "source-port-identity.json").write_bytes((json.dumps(record, indent=2) + "\n").encode())
    print(json.dumps({"publisher_payloads_matched": len(compared), "declarations_changed": len(replacements),
                      "original_case_and_runtime_source_edits": False}), flush=True)


def plan():
    # Hosted metadata-only preflight, before running any original installer.
    from pip._vendor.packaging.markers import Marker, default_environment

    project = tomllib.loads((SDK / "pyproject.toml").read_text())["tool"]["poetry"]
    groups = {"main", "dev"}
    groups.update(name for name, value in project.get("group", {}).items() if not value.get("optional", False))
    environment = default_environment()
    environment["extra"] = ""
    lock = tomllib.loads((SDK / "poetry.lock").read_text())
    rejected = []
    allowed = []
    for package in lock["package"]:
        version = package["version"]
        if re.fullmatch(r"\d+(?:\.\d+)*(?:\.post\d+)?", version):
            continue
        relevant = groups.intersection(package["groups"])
        if not relevant:
            continue
        markers = package.get("markers")
        if isinstance(markers, str):
            applies = Marker(markers).evaluate(environment)
        elif isinstance(markers, dict):
            applies = any(Marker(markers[group]).evaluate(environment) if group in markers else True for group in relevant)
        else:
            assert markers is None
            applies = True
        if not applies:
            continue
        name = re.sub(r"[-_.]+", "-", package["name"].lower())
        row = {"name": name, "version": version, "groups": sorted(relevant), "markers": markers}
        if name == "opentelemetry-semantic-conventions" and version == "0.66b1":
            allowed.append(row)
        else:
            rejected.append(row)
    record = {"actual_python": sys.version, "selected_groups": sorted(groups),
              "approved_exact_exception": allowed, "unapproved_versions": rejected,
              "installer_commands_modified": False, "SDK_or_runtime_acceptance": False}
    (EVIDENCE / "preinstall-version-policy.json").write_bytes((json.dumps(record, indent=2) + "\n").encode())
    assert not rejected, "Unapproved versions in the current SDK installation scope; do not install"


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
    for name in ["pyproject.toml", "poetry.lock"]:
        (EVIDENCE / ("ported-" + name)).write_bytes((SDK / name).read_bytes())
    (EVIDENCE / "source-preservation-after.json").write_bytes((json.dumps({
        "changed_publisher_files": changed, "all_original_SDK_cases_helpers_and_runtime_code_unchanged": True,
        "original_publisher_commands": record["publisher_commands"], "full_vendor_acceptance": False}, indent=2) + "\n").encode())


if __name__ == "__main__":
    mode = MODE.parse_args().mode
    if mode == "prepare":
        prepare()
    elif mode == "plan":
        plan()
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
