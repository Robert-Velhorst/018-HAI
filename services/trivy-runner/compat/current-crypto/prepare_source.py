"""Prepare and reconcile a complete publisher source port on an owned hosted SDK."""
from pathlib import Path, PurePosixPath
import argparse
import hashlib
import json
import os
import re
import subprocess


def git(source, *args):
    return subprocess.check_output(["git", "-C", str(source), *args])


def snapshot(source, spec):
    assert git(source, "rev-parse", "HEAD").decode().strip() == spec["publisher_commit"]
    assert git(source, "rev-parse", "HEAD^{tree}").decode().strip() == spec["publisher_tree"]
    result = {}
    for entry in git(source, "ls-tree", "-r", "-z", "HEAD").split(b"\0"):
        if not entry:
            continue
        info, name = entry.split(b"\t", 1)
        mode, kind, blob = info.decode().split()
        name = name.decode("utf-8")
        relative = PurePosixPath(name)
        assert not relative.is_absolute() and ".." not in relative.parts
        assert kind == "blob" and mode in {"100644", "100755", "120000"}
        path = source.joinpath(*relative.parts)
        if mode == "120000":
            assert path.is_symlink()
            data = os.readlink(path).encode("utf-8")
        else:
            assert path.is_file() and not path.is_symlink()
            data = path.read_bytes()
        actual_blob = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        result[name] = {
            "publisher_blob": blob,
            "actual_blob": actual_blob,
            "sha256": hashlib.sha256(data).hexdigest(),
            "mode": mode,
        }
    assert len(result) == spec["publisher_blob_count"]
    return result


def json_stream(text):
    decoder = json.JSONDecoder()
    result = []
    index = 0
    while index < len(text):
        if text[index].isspace():
            index += 1
            continue
        value, index = decoder.raw_decode(text, index)
        result.append(value)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--phase", choices=["prepare", "verify"], required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--reports", type=Path, required=True)
    args = parser.parse_args()
    assert os.environ.get("GITHUB_ACTIONS") == "true"
    assert os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
    assert os.environ.get("RUNNER_OS") == "Linux"
    spec = json.loads(Path(__file__).with_name("spec.json").read_text())
    source = args.source.resolve(strict=True)
    reports = args.reports.resolve()
    assert reports != source and not reports.is_relative_to(source)
    reports.mkdir(parents=True, exist_ok=True)
    current = snapshot(source, spec)
    if args.phase == "prepare":
        assert not git(source, "status", "--porcelain").strip()
        assert all(row["actual_blob"] == row["publisher_blob"] for row in current.values())
        (reports / "publisher-payloads.json").write_text(json.dumps(current, indent=2) + "\n")
        path = source / "go.mod"
        original = path.read_bytes()
        assert hashlib.sha256(original).hexdigest() == spec["publisher_go_mod_sha256"]
        old = ("\t" + spec["module"] + " " + spec["previous_module_version"] + "\n").encode()
        new = ("\t" + spec["module"] + " " + spec["candidate_module_version"] + "\n").encode()
        assert original.count(old) == 1
        prepared = original.replace(old, new, 1)
        assert hashlib.sha256(prepared).hexdigest() == spec["prepared_go_mod_sha256"]
        path.write_bytes(prepared)
        (reports / "publisher-go.mod").write_bytes(original)
        (reports / "prepared-go.mod").write_bytes(prepared)
        (reports / "source-identity.json").write_text(json.dumps({
            "project_worker_head": os.environ["GITHUB_SHA"],
            "publisher_commit": spec["publisher_commit"],
            "publisher_tree": spec["publisher_tree"],
            "original_payloads_verified": len(current),
            "original_cases_or_helpers_edited": False,
            "operator_data_accessed": False,
            "runtime_acceptance": False,
        }, indent=2) + "\n")
        print("Verified complete publisher source; prepared one direct module declaration.")
        return

    original = json.loads((reports / "publisher-payloads.json").read_text())
    assert set(original) == set(current)
    changed = {name for name in current if original[name]["sha256"] != current[name]["sha256"]}
    assert changed <= {"go.mod", "go.sum"} and "go.mod" in changed
    statuses = git(source, "status", "--porcelain", "-z").split(b"\0")
    assert all(entry[3:].decode() in {"go.mod", "go.sum"} for entry in statuses if entry)
    mod = (source / "go.mod").read_text()
    assert re.search(r"(?m)^\s*" + re.escape(spec["module"]) + r"\s+" +
                     re.escape(spec["candidate_module_version"]) + r"\s*$", mod)
    before = json.loads((reports / "prepared-module-metadata.json").read_text())
    after = json.loads((reports / "resolved-module-metadata.json").read_text())
    assert (before.get("Replace") or []) == (after.get("Replace") or [])
    modules = json_stream((reports / "complete-module-graph.json").read_text())
    selected = [row for row in modules if row["Path"] == spec["module"]]
    assert len(selected) == 1 and selected[0]["Version"] == spec["candidate_module_version"]
    assert "Replace" not in selected[0]
    environment = json.loads((reports / "go-environment.json").read_text())
    assert environment["GOVERSION"] == "go" + spec["go_version"]
    assert environment["GOSUMDB"] and environment["GOSUMDB"] != "off"
    for name in ["go.mod", "go.sum"]:
        (reports / ("resolved-" + name)).write_bytes((source / name).read_bytes())
    receipt = {
        "publisher_commit": spec["publisher_commit"],
        "selected_module": selected[0],
        "original_payloads_verified": len(current),
        "all_original_code_case_helper_fixture_payloads_preserved": True,
        "unchanged_publisher_payloads": len(current) - len(changed),
        "changed_dependency_input_files": sorted(changed),
        "normal_module_graph_count": len(modules),
        "new_module_replacement_added": False,
        "normal_module_resolution_completed": True,
        "complete_original_vendor_SDK_executed": False,
        "current_runtime_binary_or_image_qualified": False,
        "all_transitive_components_highest_stable": False,
        "GO_2026_5932_resolved_or_waived": False,
        "deployment_or_operator_data_acceptance": False,
    }
    (reports / "normal-resolution-receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print("Normal current-module resolution reconciled; full original SDK and image gates remain open.")


if __name__ == "__main__":
    main()
