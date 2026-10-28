"""Add one owned generator to an owned image; preserve every upstream byte."""
import base64
import hashlib
import importlib
from importlib.metadata import distribution, version
import json
from pathlib import Path

assert version("garak") == "0.17.1"
assert version("openai") == "3.37.0"
dist = distribution("garak")
original = {}
for entry in dist.files:
    path = Path(dist.locate_file(entry))
    if not path.is_file():
        continue
    raw = path.read_bytes()
    if entry.hash is not None:
        assert entry.hash.mode == "sha256"
        assert base64.urlsafe_b64encode(hashlib.sha256(raw).digest()).decode().rstrip("=") == entry.hash.value
    original[str(entry)] = hashlib.sha256(raw).hexdigest()
source = Path(__file__).with_name("hai_local.py")
target = Path(dist.locate_file("garak/generators/hai_local.py"))
assert not target.exists()
assert target.parent.is_dir()
raw = source.read_bytes()
target.write_bytes(raw)
assert target.read_bytes() == raw
plugin = importlib.import_module("garak.generators.hai_local")
parent = importlib.import_module("garak.generators.openai")
assert issubclass(plugin.LocalOpenAICompatible, parent.OpenAICompatible)
for name, digest in original.items():
    assert hashlib.sha256(Path(dist.locate_file(name)).read_bytes()).hexdigest() == digest
out = Path("/usr/share/doc/hai-garak-local-generator")
out.mkdir(parents=True, exist_ok=True)
(out / "source-identity.json").write_text(json.dumps({
    "garak": version("garak"), "openai": version("openai"),
    "original_distribution_files_unchanged": original,
    "owned_additive_module_sha256": hashlib.sha256(raw).hexdigest(),
    "original_vendor_files_modified": False,
    "full_dependency_graph_or_SDK_qualified": False,
}, indent=2) + "\n", encoding="utf-8")
