"""Qualify public SDK imports in a network-disabled disposable Linux SDK."""
from pathlib import Path
import argparse
import hashlib
from importlib.metadata import distribution, version
import inspect
import json
import os
import platform
import sys
import sysconfig


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--site-packages", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    args = parser.parse_args()
    if (os.environ.get("HAI_DISPOSABLE_PUBLIC_SDK") != "1" or sys.platform != "linux"
            or sys.version_info[:2] != (3, 15) or sysconfig.get_config_var("Py_GIL_DISABLED")
            or args.site_packages.resolve() != Path("/installed")
            or args.receipt != Path("/qualification/sdk-import.json")):
        raise RuntimeError("Requires the fixed disposable CPython 3.15 Linux SDK layout")
    sys.path.insert(0, str(args.site_packages))
    expected = {"mini-swe-agent": "2.4.6", "litellm": "1.106.3+hai.py315.1",
                "filelock": "4.2.4", "openai": "3.37.0", "huggingface-hub": "2.3.0",
                "importlib-metadata": "10.1.0", "fastuuid": "0.14.0+hai.py315.1"}
    observed = {name: version(name) for name in expected}
    if observed != expected:
        raise ValueError("Installed SDK identities differ from the reviewed candidate")
    package = distribution("litellm")
    receipt_file = next(p for p in package.files if str(p).endswith(".dist-info/HAI_PORT.json"))
    spec = json.loads(receipt_file.locate().read_text(encoding="utf-8"))
    sources = {str(p): hashlib.sha256(p.locate().read_bytes()).hexdigest()
               for p in package.files if str(p).startswith("litellm/") and str(p).endswith(".py")}
    manifest = hashlib.sha256(json.dumps(sources, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    if len(sources) != spec["unchanged_python_file_count"] or manifest != spec["unchanged_python_manifest_sha256"]:
        raise ValueError("Installed LiteLLM Python sources differ from upstream")
    for native in spec["native_members"]:
        path = next(p for p in package.files if str(p) == native["path"])
        if hashlib.sha256(path.locate().read_bytes()).hexdigest() != native["sha256"]:
            raise ValueError("Installed native bridge differs from upstream")
    if os.environ.get("LITELLM_LOCAL_MODEL_COST_MAP", "").lower() != "true":
        raise RuntimeError("Only the bundled public model-cost map is permitted")
    fastuuid_proof = json.loads(Path("/inputs/fastuuid-build.json").read_text())
    if fastuuid_proof["local_version"] != observed["fastuuid"] or len(fastuuid_proof["native_members"]) != 1:
        raise ValueError("FastUUID build identity differs")
    fastuuid_distribution = distribution("fastuuid")
    for native in fastuuid_proof["native_members"]:
        path = next(p for p in fastuuid_distribution.files if str(p) == native["path"])
        if hashlib.sha256(path.locate().read_bytes()).hexdigest() != native["sha256"]:
            raise ValueError("Installed FastUUID extension differs from the source-bound build")
    # These imports execute only in the public, credential-free hosted SDK.
    # No model, provider, agent instance or application case is invoked.
    import filelock
    import fastuuid
    import litellm
    import litellm.rust_bridge._native
    import openai
    from minisweagent.agents.default import DefaultAgent
    from minisweagent.models.litellm_model import LitellmModel

    parameters = inspect.signature(filelock.BaseFileLock.__init__).parameters
    if not {"lock_file", "timeout", "mode"}.issubset(parameters):
        raise ValueError("Filelock setup API differs")
    if not callable(litellm.completion) or not callable(openai.OpenAI) or not isinstance(DefaultAgent, type) or not isinstance(LitellmModel, type):
        raise ValueError("Expected real SDK/agent/model interfaces were not imported")
    if not isinstance(fastuuid.UUID, type):
        raise ValueError("The real FastUUID extension did not provide its UUID class")
    result = {"python": platform.python_version(), "standard_GIL_build": True,
              "distributions": observed, "vendor_sdk_imports_pass": True,
              "native_bridge_loaded": True, "native_bridge_sha256_verified": True,
              "fastuuid_native_extension_loaded_and_hash_verified": True,
              "upstream_python_files_unchanged": len(sources), "filelock_setup_signature_present": True,
              "application_cases_run": False, "model_or_agent_instantiated": False,
              "private_application_source_or_credentials_mounted": False,
              "full_behavioral_native_security_or_provider_acceptance": False}
    args.receipt.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
