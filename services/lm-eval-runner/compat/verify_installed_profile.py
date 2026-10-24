"""Verify installed LM Eval source identity without importing the engine."""
from importlib.metadata import distribution
from importlib.util import find_spec
from pathlib import Path
import hashlib
import json


def main():
    spec = json.loads(Path(__file__).with_name("port-spec.json").read_text(encoding="utf-8"))
    installed = distribution("lm-eval")
    if installed.version != spec["local_version"] or find_spec("sqlitedict") is not None:
        raise ValueError("Unexpected LM Eval version or prohibited SqliteDict installation")
    sources = {str(path): hashlib.sha256(path.locate().read_bytes()).hexdigest()
               for path in installed.files if str(path).startswith("lm_eval/") and str(path).endswith(".py")}
    manifest = hashlib.sha256(json.dumps(sources, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    if len(sources) != spec["unchanged_python_file_count"] or manifest != spec["unchanged_python_manifest_sha256"]:
        raise ValueError("Installed evaluation engine source differs from upstream")
    entrypoints = {e.name: e.value for e in installed.entry_points if e.group == "console_scripts"}
    if entrypoints != {"lm-eval": "lm_eval.__main__:cli_evaluate", "lm_eval": "lm_eval.__main__:cli_evaluate"}:
        raise ValueError("Evaluation CLI entrypoints changed")
    parent_receipts = []
    parents = json.loads(Path(__file__).with_name("python-parent-port-specs.json").read_text(encoding="utf-8"))
    for parent in parents:
        candidate = distribution(parent["name"])
        if candidate.version != parent["local_version"]:
            raise ValueError("Installed parent distribution version differs: " + parent["name"])
        files = {str(path): hashlib.sha256(path.locate().read_bytes()).hexdigest()
                 for path in candidate.files if str(path).endswith(".py")}
        digest = hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        if len(files) != parent["unchanged_python_file_count"] or digest != parent["unchanged_python_manifest_sha256"]:
            raise ValueError("Installed parent Python sources differ: " + parent["name"])
        parent_receipts.append({"name": parent["name"], "version": candidate.version,
                                "unchanged_python_files": len(files), "source_manifest_sha256": digest})
    print(json.dumps({"engine": installed.version, "sqlitedict_installed": False,
                      "unchanged_python_files": len(sources), "source_manifest_sha256": manifest,
                      "cli_entrypoints_preserved": True, "parents": parent_receipts, "vendor_code_executed": False}))


if __name__ == "__main__":
    main()
