"""Repack verified public LM Eval metadata without importing vendor code."""
from pathlib import Path, PurePosixPath
import argparse
import base64
import csv
import email
import hashlib
import io
import json
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener
import zipfile


def sha256(value):
    return hashlib.sha256(value).hexdigest()


def public_url(value):
    url = urlsplit(value)
    if (url.scheme != "https" or url.hostname != "files.pythonhosted.org"
            or url.username or url.password or url.port not in {None, 443}):
        raise ValueError("Unexpected upstream wheel location")


class PublicRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        public_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-directory", type=Path, required=True)
    parser.add_argument("--upstream-wheel", type=Path)
    args = parser.parse_args()
    spec = json.loads(Path(__file__).with_name("port-spec.json").read_text(encoding="utf-8"))
    if args.upstream_wheel:
        raw = args.upstream_wheel.read_bytes()
    else:
        public_url(spec["upstream_url"])
        with build_opener(PublicRedirects()).open(spec["upstream_url"], timeout=60) as response:
            public_url(response.url)
            raw = response.read(16 * 1024 * 1024 + 1)
    if len(raw) > 16 * 1024 * 1024 or sha256(raw) != spec["upstream_wheel_sha256"]:
        raise ValueError("Upstream wheel does not match the reviewed release")
    files = {}
    modes = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist()) > 20000 or sum(m.file_size for m in archive.infolist()) > 32 * 1024 * 1024:
            raise ValueError("Unexpected archive bounds")
        for member in archive.infolist():
            path = PurePosixPath(member.filename)
            if path.is_absolute() or ".." in path.parts or "\\" in member.filename:
                raise ValueError("Unexpected archive member path")
            if member.is_dir():
                continue
            if member.filename in files or (member.external_attr >> 16) & 0o170000 == 0o120000:
                raise ValueError("Duplicate or linked archive member")
            files[member.filename] = archive.read(member)
            modes[member.filename] = member.external_attr >> 16
    old = "lm_eval-" + spec["upstream_version"] + ".dist-info"
    new = "lm_eval-" + spec["local_version"] + ".dist-info"
    records = list(csv.reader(io.StringIO(files[old + "/RECORD"].decode())))
    if len(records) != len(files) or {row[0] for row in records} != set(files):
        raise ValueError("Upstream RECORD membership differs")
    for name, digest, size in records:
        if name == old + "/RECORD":
            if digest or size:
                raise ValueError("Unexpected self RECORD entry")
            continue
        actual = "sha256=" + base64.urlsafe_b64encode(hashlib.sha256(files[name]).digest()).rstrip(b"=").decode()
        if digest != actual or int(size) != len(files[name]):
            raise ValueError("Upstream RECORD content differs: " + name)
    sources = {name: sha256(data) for name, data in sorted(files.items()) if name.endswith(".py")}
    if (len(sources) != spec["unchanged_python_file_count"]
            or sha256(json.dumps(sources, sort_keys=True, separators=(",", ":")).encode()) != spec["unchanged_python_manifest_sha256"]):
        raise ValueError("Original evaluation engine sources differ")
    original_metadata = files[old + "/METADATA"].replace(b"\r\n", b"\n")
    metadata = email.message_from_bytes(original_metadata)
    if metadata["Name"] != "lm_eval" or metadata["Version"] != spec["upstream_version"]:
        raise ValueError("Unexpected distribution identity")
    header, separator, payload = original_metadata.partition(b"\n\n")
    if not separator or header.splitlines().count(b"Requires-Dist: sqlitedict") != 1:
        raise ValueError("Expected one unconditional SQLite cache requirement")
    if any(line.lower().startswith(b"provides-extra: sqlite-response-cache") for line in header.splitlines()):
        raise ValueError("SQLite cache extra already exists")
    lines = []
    for line in header.splitlines():
        if line.startswith(b"Version:"):
            line = ("Version: " + spec["local_version"]).encode()
        elif line == b"Requires-Dist: sqlitedict":
            line = b'Requires-Dist: sqlitedict; extra == "sqlite-response-cache"'
        lines.append(line)
    lines.append(b"Provides-Extra: sqlite-response-cache")
    notice = ("\n\nHAI metadata port: " + spec["profile"] + "\n").encode()
    files[old + "/METADATA"] = b"\n".join(lines) + b"\n\n" + payload + notice
    wheel = files[old + "/WHEEL"].decode().splitlines()
    files[old + "/WHEEL"] = ("\n".join(
        "Generator: HAI optional SQLite cache metadata port" if line.startswith("Generator:") else line
        for line in wheel) + "\n").encode()
    # Preserve every engine module, task/resource, license and CLI entrypoint.
    if {name: sha256(files[name]) for name in sources} != sources:
        raise ValueError("An evaluation engine source was modified")
    files.pop(old + "/RECORD")
    files = {name.replace(old + "/", new + "/", 1): data for name, data in files.items()}
    modes = {name.replace(old + "/", new + "/", 1): mode for name, mode in modes.items()}
    receipt = {**spec, "vendor_code_executed": False, "python_sources_modified": 0,
               "runtime_qualified": False}
    files[new + "/HAI_PORT.json"] = (json.dumps(receipt, indent=2) + "\n").encode()
    stream = io.StringIO(newline="")
    writer = csv.writer(stream, lineterminator="\n")
    for name, data in sorted(files.items()):
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
        writer.writerow([name, "sha256=" + digest, len(data)])
    writer.writerow([new + "/RECORD", "", ""])
    files[new + "/RECORD"] = stream.getvalue().encode()
    result = io.BytesIO()
    with zipfile.ZipFile(result, "w", compression=zipfile.ZIP_STORED) as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = modes.get(name, 0o100644) << 16
            archive.writestr(info, data)
    output = args.output_directory / ("lm_eval-" + spec["local_version"] + "-py3-none-any.whl")
    args.output_directory.mkdir(parents=True, exist_ok=True)
    with output.open("xb") as destination:
        destination.write(result.getvalue())
    print(json.dumps({"filename": output.name, "sha256": sha256(result.getvalue()),
                      "upstream_sha256": spec["upstream_wheel_sha256"],
                      "unchanged_python_files": len(sources), "python_sources_modified": 0,
                      "optional_cache_extra": "sqlite-response-cache", "vendor_code_executed": False,
                      "runtime_qualified": False}))


if __name__ == "__main__":
    main()
