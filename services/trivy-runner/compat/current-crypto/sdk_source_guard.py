"""Bind original SDK jobs to one resolved public-source port; execute no SDK cases."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import subprocess
import sys


def git(source, *args):
    return subprocess.check_output(["git", "-C", str(source), *args])


def payloads(source, spec):
    assert git(source, "rev-parse", spec["publisher_commit"] + "^{tree}").decode().strip() == spec["publisher_tree"]
    result = {}
    for entry in git(source, "ls-tree", "-r", "-z", spec["publisher_commit"]).split(b"\0"):
        if not entry:
            continue
        info, raw_name = entry.split(b"\t", 1)
        mode, kind, blob = info.decode().split()
        name = raw_name.decode("utf-8")
        path = source / name
        assert kind == "blob" and mode in {"100644", "100755", "120000"}
        if mode == "120000":
            if path.is_symlink():
                data = os.readlink(path).encode("utf-8")
                representation = "native-symlink"
            else:
                # Git's normal Windows checkout represents a tracked symlink's
                # exact target bytes as a file when core.symlinks is false.
                assert os.name == "nt" and path.is_file()
                setting = subprocess.run(
                    ["git", "-C", str(source), "config", "--get", "core.symlinks"],
                    capture_output=True,
                )
                assert setting.stdout.strip() in {b"", b"false"}
                data = path.read_bytes()
                representation = "Windows-Git-symlink-target-file"
        else:
            assert path.is_file() and not path.is_symlink()
            data = path.read_bytes()
            representation = "regular-file"
        result[name] = {
            "publisher_blob": blob,
            "actual_blob": hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest(),
            "sha256": hashlib.sha256(data).hexdigest(),
            "mode": mode,
            "representation": representation,
            "executable": bool(path.stat().st_mode & 0o111) if os.name != "nt" and mode != "120000" else None,
        }
    assert len(result) == spec["publisher_blob_count"]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--phase", choices=["adopt", "verify"], required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--metadata", type=Path, required=True)
    parser.add_argument("--reports", type=Path, required=True)
    args = parser.parse_args()
    assert os.environ.get("GITHUB_ACTIONS") == "true"
    assert os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
    assert os.environ.get("RUNNER_OS") in {"Linux", "Windows", "macOS"}
    assert sys.version_info[:3] == (3, 15, 1)
    spec = json.loads(Path(__file__).with_name("spec.json").read_text())
    source = args.source.resolve(strict=True)
    metadata = args.metadata.resolve(strict=True)
    reports = args.reports.resolve()
    assert not reports.is_relative_to(source) and not metadata.is_relative_to(source)
    reports.mkdir(parents=True, exist_ok=True)
    if args.phase == "adopt":
        environment = json.loads(subprocess.check_output(
            ["go", "env", "-json", "GOVERSION", "GOSUMDB", "GOOS", "GOARCH", "GOMOD"],
            cwd=source,
        ))
        assert environment["GOVERSION"] == "go" + spec["go_version"]
        assert environment["GOSUMDB"] and environment["GOSUMDB"] != "off"
        (reports / "sdk-go-environment.json").write_text(json.dumps(environment, indent=2) + "\n")
        assert git(source, "rev-parse", "HEAD").decode().strip() == spec["publisher_commit"]
        assert not git(source, "status", "--porcelain").strip()
        original = payloads(source, spec)
        assert all(row["actual_blob"] == row["publisher_blob"] for row in original.values())
        identity = json.loads((metadata / "source-identity.json").read_text())
        receipt = json.loads((metadata / "normal-resolution-receipt.json").read_text())
        assert identity["project_worker_head"] == os.environ["GITHUB_SHA"]
        assert identity["publisher_commit"] == spec["publisher_commit"]
        assert identity["publisher_tree"] == spec["publisher_tree"]
        assert receipt["normal_module_resolution_completed"] is True
        assert receipt["new_module_replacement_added"] is False
        assert receipt["selected_module"]["Path"] == spec["module"]
        assert receipt["selected_module"]["Version"] == spec["candidate_module_version"]
        for name in ["go.mod", "go.sum"]:
            target = source / name
            assert target.is_file() and not target.is_symlink()
            target.write_bytes((metadata / ("resolved-" + name)).read_bytes())
        adopted = payloads(source, spec)
        changed = {name for name in adopted if adopted[name]["sha256"] != original[name]["sha256"]}
        assert changed <= {"go.mod", "go.sum"} and "go.mod" in changed
        assert all(adopted[name] == original[name] for name in adopted if name not in changed)
        git(source, "add", "--", "go.mod", "go.sum")
        git(source, "-c", "user.name=github-actions[bot]",
            "-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com",
            "commit", "-m", "Owned ephemeral Trivy crypto-port SDK snapshot; never pushed")
        ancestry = git(source, "rev-list", "--parents", "-n", "1", "HEAD").decode().split()
        assert len(ancestry) == 2 and ancestry[1] == spec["publisher_commit"]
        assert not git(source, "status", "--porcelain").strip()
        snapshot = {
            "project_worker_head": os.environ["GITHUB_SHA"],
            "run_id": os.environ["GITHUB_RUN_ID"],
            "publisher_commit": spec["publisher_commit"],
            "port_snapshot_commit": ancestry[0],
            "port_snapshot_tree": git(source, "rev-parse", "HEAD^{tree}").decode().strip(),
            "publisher_payloads": len(original),
            "changed_dependency_input_files": sorted(changed),
            "resolved_input_sha256": {name: adopted[name]["sha256"] for name in ["go.mod", "go.sum"]},
            "ephemeral_snapshot_never_pushed": True,
            "publisher_cases_assertions_helpers_fixtures_changed": False,
            "complete_original_SDK_results_still_required": True,
        }
        (reports / "sdk-input-payloads.json").write_text(json.dumps(adopted, indent=2) + "\n")
        (reports / "sdk-source-snapshot.json").write_text(json.dumps(snapshot, indent=2) + "\n")
        print("Complete publisher source adopted with the same resolved dependency inputs; original SDK results remain required.")
        return

    snapshot = json.loads((reports / "sdk-source-snapshot.json").read_text())
    assert snapshot["project_worker_head"] == os.environ["GITHUB_SHA"]
    assert snapshot["run_id"] == os.environ["GITHUB_RUN_ID"]
    assert git(source, "rev-parse", "HEAD").decode().strip() == snapshot["port_snapshot_commit"]
    assert git(source, "rev-parse", "HEAD^{tree}").decode().strip() == snapshot["port_snapshot_tree"]
    expected = json.loads((reports / "sdk-input-payloads.json").read_text())
    actual = payloads(source, spec)
    assert expected == actual
    assert not git(source, "diff", "--name-only").strip()
    assert not git(source, "diff", "--cached", "--name-only").strip()
    built = {}
    for path in (source / "dist").rglob("*"):
        if path.is_file() and not path.is_symlink():
            assert path.resolve().is_relative_to(source)
            with path.open("rb") as stream:
                built[path.relative_to(source).as_posix()] = {
                    "bytes": path.stat().st_size,
                    "sha256": hashlib.file_digest(stream, "sha256").hexdigest(),
                }
    (reports / "sdk-built-output-hashes.json").write_text(json.dumps({
        "outputs": built,
        "physical_final_runtime_image_acceptance": False,
    }, indent=2) + "\n")
    (reports / "sdk-source-integrity-after.json").write_text(json.dumps({
        "project_worker_head": os.environ["GITHUB_SHA"],
        "all_port_input_payloads_unchanged": len(actual),
        "publisher_original_cases_helpers_assertions_fixtures_unchanged": True,
        "SDK_case_results_and_runtime_image_acceptance_are_separate": True,
    }, indent=2) + "\n")
    print("All SDK source and dependency inputs remain unchanged; case outcomes are separate gates.")


if __name__ == "__main__":
    main()
