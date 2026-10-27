"""Build an explicitly modified Evidently wheel using reviewed byte patches.

Only standard-library source/archive operations run here; the SDK is not imported.
"""
from pathlib import Path, PurePosixPath
from urllib.request import HTTPRedirectHandler, build_opener
from urllib.parse import urlsplit
import argparse
import ast
import base64
import csv
import email
import hashlib
import io
import json
import zipfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def check_url(value):
    parsed = urlsplit(value)
    if (parsed.scheme != "https" or parsed.hostname != "files.pythonhosted.org"
            or parsed.username or parsed.password or parsed.port not in {None, 443}):
        raise ValueError("Unexpected upstream wheel source")


class Redirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        check_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-directory", type=Path, required=True)
    parser.add_argument("--upstream-wheel", type=Path)
    args = parser.parse_args()
    spec = json.loads(Path(__file__).with_name("evidently-port-spec.json").read_text(encoding="utf-8"))
    if args.upstream_wheel:
        raw = args.upstream_wheel.read_bytes()
    else:
        check_url(spec["upstream_url"])
        with build_opener(Redirects()).open(spec["upstream_url"], timeout=60) as response:
            check_url(response.url)
            raw = response.read(16 * 1024 * 1024 + 1)
    if len(raw) > 16 * 1024 * 1024 or sha(raw) != spec["upstream_wheel_sha256"]:
        raise ValueError("Upstream wheel differs")
    files = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist()) > 2000 or sum(x.file_size for x in archive.infolist()) > 64 * 1024 * 1024:
            raise ValueError("Unexpected wheel bounds")
        for member in archive.infolist():
            path = PurePosixPath(member.filename)
            if path.is_absolute() or ".." in path.parts or "\\" in member.filename:
                raise ValueError("Unexpected wheel member")
            if not member.is_dir():
                if member.filename in files:
                    raise ValueError("Duplicate wheel member")
                files[member.filename] = archive.read(member)
    old_info = "evidently-" + spec["upstream_version"] + ".dist-info"
    new_info = "evidently-" + spec["local_version"] + ".dist-info"
    metadata = email.message_from_bytes(files[old_info + "/METADATA"])
    if metadata["Name"] != "evidently" or metadata["Version"] != spec["upstream_version"]:
        raise ValueError("Unexpected source package metadata")
    if {n: sha(v) for n, v in files.items() if n.endswith(".py")} != spec["upstream_python_modules"]:
        raise ValueError("Upstream Python modules differ")
    changed = {}
    for name, patch in spec["source_patches"].items():
        value = files[name]
        if sha(value) != patch["original_sha256"]:
            raise ValueError("Original patch target differs: " + name)
        for edit in patch["edits"]:
            old, new = edit["old"].encode(), edit["new"].encode()
            if value.count(old) != 1:
                raise ValueError("Ambiguous source edit: " + name)
            value = value.replace(old, new, 1)
        if sha(value) != patch["patched_sha256"]:
            raise ValueError("Patched source identity differs: " + name)
        ast.parse(value)
        changed[name] = sha(value)
        files[name] = value
    header, separator, body = files[old_info + "/METADATA"].replace(b"\r\n", b"\n").partition(b"\n\n")
    if not separator:
        raise ValueError("Missing metadata body")
    lines = header.splitlines()
    old_version = ("Version: " + spec["upstream_version"]).encode()
    old_plotly = b"Requires-Dist: plotly<6,>=5.10.0"
    if lines.count(old_version) != 1 or lines.count(old_plotly) != 1:
        raise ValueError("Unexpected version or Plotly requirement")
    lines[lines.index(old_version)] = ("Version: " + spec["local_version"]).encode()
    lines[lines.index(old_plotly)] = b"Requires-Dist: plotly==7.1.0"
    files[old_info + "/METADATA"] = b"\n".join(lines) + b"\n\n" + body + b"\n\nHAI modified Plotly 7 compatibility port; see HAI_PORT.json.\n"
    wheel = files[old_info + "/WHEEL"].decode().splitlines()
    files[old_info + "/WHEEL"] = ("\n".join("Generator: HAI source-bound Evidently port" if x.startswith("Generator:") else x for x in wheel) + "\n").encode()
    del files[old_info + "/RECORD"]
    files = {n.replace(old_info + "/", new_info + "/", 1): v for n, v in files.items()}
    manifest = {k: v for k, v in spec.items() if k != "source_patches"}
    manifest["source_patches"] = {n: {k: v for k, v in p.items() if k != "edits"} for n, p in spec["source_patches"].items()}
    manifest["vendor_code_executed_during_build"] = False
    files[new_info + "/HAI_PORT.json"] = (json.dumps(manifest, indent=2) + "\n").encode()
    record = io.StringIO(newline="")
    writer = csv.writer(record, lineterminator="\n")
    for name, value in sorted(files.items()):
        encoded = base64.urlsafe_b64encode(hashlib.sha256(value).digest()).rstrip(b"=").decode()
        writer.writerow([name, "sha256=" + encoded, len(value)])
    writer.writerow([new_info + "/RECORD", "", ""])
    files[new_info + "/RECORD"] = record.getvalue().encode()
    stream = io.BytesIO()
    with zipfile.ZipFile(stream, "w", compression=zipfile.ZIP_STORED) as archive:
        for name, value in sorted(files.items()):
            info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_STORED
            archive.writestr(info, value)
    args.output_directory.mkdir(parents=True, exist_ok=True)
    filename = "evidently-" + spec["local_version"] + "-py3-none-any.whl"
    with (args.output_directory / filename).open("xb") as output:
        output.write(stream.getvalue())
    print(json.dumps({"filename": filename, "sha256": sha(stream.getvalue()),
                      "upstream_sha256": spec["upstream_wheel_sha256"],
                      "patched_modules": changed,
                      "unchanged_python_modules": len(spec["upstream_python_modules"]) - len(changed),
                      "vendor_code_executed": False, "runtime_qualified": False}))


if __name__ == "__main__":
    main()
