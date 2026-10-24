"""Apply a bounded compatibility import to the installed public SDK package.

Run only in the disposable CI interpreter or container build, before imports.
The original release/module hashes are retained in the audit documentation.
"""
import argparse
import base64
import csv
import hashlib
from importlib.metadata import distribution
import io
import json
from pathlib import Path

MODULE = "evidently/legacy/tests/data_integrity_tests.py"
ORIGINAL_SHA256 = "a2754fa739125076375915530483ba6c38ec3035197db82e8a20a83e3b84f0d1"
OLD = b"from pandas.core.dtypes.common import infer_dtype_from_object  # type: ignore[attr-defined]"
NEW = b"from compat_pandas import infer_dtype_from_object  # HAI pandas 3.1 compatibility"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply-to-sdk", required=True, action="store_true")
    parser.parse_args()
    installed = distribution("evidently")
    if installed.version != "0.7.23":
        raise RuntimeError("Review the compatibility patch for the new Evidently release")
    path = Path(installed.locate_file(MODULE))
    source = path.read_bytes()
    adapter = Path(__file__).with_name("compat_pandas.py")
    original = hashlib.sha256(source).hexdigest()
    if original == ORIGINAL_SHA256 and source.count(OLD) == 1:
        updated = source.replace(OLD, NEW, 1)
    elif source.count(NEW) == 1:
        # Accept only the exact previously patched original, not arbitrary code.
        restored = source.replace(NEW, OLD, 1)
        if hashlib.sha256(restored).hexdigest() != ORIGINAL_SHA256:
            raise RuntimeError("Installed Evidently code does not match the reviewed release")
        updated = source
    else:
        raise RuntimeError("Installed Evidently code does not match the reviewed release")
    files = list(installed.files or [])
    records = [f for f in files if str(f).endswith(".dist-info/RECORD")]
    if len(records) != 1:
        raise RuntimeError("Expected one installed distribution RECORD")
    record_path = Path(installed.locate_file(records[0]))
    rows = list(csv.reader(io.StringIO(record_path.read_text(encoding="utf-8"))))
    matches = [row for row in rows if row[0] == MODULE]
    if len(matches) != 1:
        raise RuntimeError("Expected one source module RECORD entry")
    digest = hashlib.sha256(updated).digest()
    matches[0][1] = "sha256=" + base64.urlsafe_b64encode(digest).rstrip(b"=").decode("ascii")
    matches[0][2] = str(len(updated))
    output = io.StringIO(newline="")
    csv.writer(output, lineterminator="\n").writerows(rows)
    # These are the two explicitly owned SDK package files; no source tree or
    # operator environment is modified by merely importing this helper.
    path.write_bytes(updated)
    record_path.write_text(output.getvalue(), encoding="utf-8", newline="")
    print(json.dumps({"distribution": "evidently", "version": installed.version,
                      "module": MODULE, "reviewed_original_sha256": ORIGINAL_SHA256,
                      "patched_sha256": digest.hex(), "installed_record_updated": True,
                      "adapter_sha256": hashlib.sha256(adapter.read_bytes()).hexdigest()}))


if __name__ == "__main__":
    main()
