"""Apply reviewed CMake/package edits inside a disposable OR-Tools build SDK.

This standard-library helper does not import the solver or run application cases.
"""
from pathlib import Path
import argparse
import hashlib
import json
import os


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--component", choices=["ortools", "protobuf"], required=True)
    parser.add_argument("--source", type=Path, required=True)
    args = parser.parse_args()
    if os.environ.get("HAI_DISPOSABLE_ORTOOLS_BUILD") != "1":
        raise RuntimeError("This operation requires an explicitly marked disposable build SDK")
    root = args.source.resolve(strict=True)
    if not root.is_dir() or args.source.is_symlink():
        raise ValueError("Expected a real source directory")
    spec = json.loads(Path(__file__).with_name("source-port-spec.json").read_text(encoding="utf-8"))
    changes = []
    prepared = []
    for name, patch in spec["components"][args.component]["patches"].items():
        path = root / name
        if path.is_symlink() or not path.resolve(strict=True).is_relative_to(root):
            raise ValueError("Patch target escapes the source root")
        original = path.read_bytes()
        if hashlib.sha256(original).hexdigest() != patch["original_sha256"]:
            raise ValueError("Original source differs: " + name)
        value = original
        for edit in patch["edits"]:
            before, after = edit["old"].encode(), edit["new"].encode()
            if value.count(before) != 1:
                raise ValueError("Source edit is ambiguous: " + name)
            value = value.replace(before, after, 1)
        digest = hashlib.sha256(value).hexdigest()
        if digest != patch["patched_sha256"]:
            raise ValueError("Modified source differs: " + name)
        prepared.append((path, value))
        changes.append({"path": name, "original_sha256": patch["original_sha256"],
                        "patched_sha256": digest})
    # Validate every target before writing any of the explicitly owned files.
    for path, value in prepared:
        path.write_bytes(value)
    receipt = {"component": args.component, "source_port_version": spec["port_version"],
               "source_identity": {k: v for k, v in spec["components"][args.component].items() if k != "patches"},
               "modified_files": changes, "solver_code_imported_or_executed": False,
               "operator_sdk_or_database_modified": False}
    (root / "HAI_PORT.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(receipt))


if __name__ == "__main__":
    main()
