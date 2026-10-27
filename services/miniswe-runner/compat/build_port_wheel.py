"""Prepare verified Linux LiteLLM compatibility metadata without vendor execution."""
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
            raw = response.read(64 * 1024 * 1024 + 1)
    if len(raw) > 64 * 1024 * 1024 or sha256(raw) != spec["upstream_wheel_sha256"]:
        raise ValueError("Upstream wheel does not match the reviewed release")
    files = {}
    modes = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist()) > 20000 or sum(m.file_size for m in archive.infolist()) > 192 * 1024 * 1024:
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
    old = "litellm-" + spec["upstream_version"] + ".dist-info"
    new = "litellm-" + spec["local_version"] + ".dist-info"
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
    if (len(sources) != spec["upstream_python_file_count"]
            or sha256(json.dumps(sources, sort_keys=True, separators=(",", ":")).encode()) != spec["upstream_python_manifest_sha256"]):
        raise ValueError("Original evaluation engine sources differ")
    for name, patch in spec["source_patches"].items():
        if name not in sources or sha256(files[name]) != patch["original_sha256"]:
            raise ValueError("Source patch target differs")
        value = files[name]
        for edit in patch["edits"]:
            before, after = edit["old"].encode(), edit["new"].encode()
            if value.count(before) != 1:
                raise ValueError("Source patch is ambiguous")
            value = value.replace(before, after, 1)
        if sha256(value) != patch["patched_sha256"]:
            raise ValueError("Modified source differs")
        files[name] = value
    ported_sources = {name: sha256(files[name]) for name in sources}
    if sha256(json.dumps(ported_sources, sort_keys=True, separators=(",", ":")).encode()) != spec["ported_python_manifest_sha256"]:
        raise ValueError("Ported Python source manifest differs")
    original_metadata = files[old + "/METADATA"].replace(b"\r\n", b"\n")
    metadata = email.message_from_bytes(original_metadata)
    if metadata["Name"] != "litellm" or metadata["Version"] != spec["upstream_version"]:
        raise ValueError("Unexpected distribution identity")
    header, separator, payload = original_metadata.partition(b"\n\n")
    if not separator:
        raise ValueError("Unexpected metadata envelope")
    for edit in spec["metadata_edits"]:
        old_line, new_line = edit["old"].encode(), edit["new"].encode()
        if header.splitlines().count(old_line) != 1:
            raise ValueError("Reviewed dependency contract differs")
        header = header.replace(old_line, new_line, 1)
    version_line = ("Version: " + spec["upstream_version"]).encode()
    if header.splitlines().count(version_line) != 1:
        raise ValueError("Unexpected original package version")
    lines = [line if line != version_line else ("Version: " + spec["local_version"]).encode()
             for line in header.splitlines()]
    notice = ("\n\nHAI compatibility candidate: " + spec["profile"] + "\n").encode()
    for member in spec["native_members"]:
        if sha256(files[member["path"]]) != member["sha256"]:
            raise ValueError("Native bridge differs from upstream")
    expected_tag = ("Tag: " + spec["wheel_tag"]).encode()
    if expected_tag not in files[old + "/WHEEL"].splitlines():
        raise ValueError("Original Linux ABI tag differs")
    if b"Root-Is-Purelib: false" not in files[old + "/WHEEL"].splitlines():
        raise ValueError("Native distribution cannot be relabeled as pure Python")
    files[old + "/METADATA"] = b"\n".join(lines) + b"\n\n" + payload + notice
    wheel = files[old + "/WHEEL"].decode().splitlines()
    files[old + "/WHEEL"] = ("\n".join(
        "Generator: HAI current-SDK compatibility candidate" if line.startswith("Generator:") else line
        for line in wheel) + "\n").encode()
    # Only the explicitly reviewed compatibility module may change. Preserve
    # all other engine modules/resources, licenses and CLI entrypoints.
    if any(sha256(files[name]) != digest for name, digest in sources.items()
           if name not in spec["source_patches"]):
        raise ValueError("An unreviewed engine source was modified")
    files.pop(old + "/RECORD")
    files = {name.replace(old + "/", new + "/", 1): data for name, data in files.items()}
    modes = {name.replace(old + "/", new + "/", 1): mode for name, mode in modes.items()}
    receipt = {**spec, "vendor_code_executed": False, "python_sources_modified": len(spec["source_patches"]),
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
    output = args.output_directory / ("litellm-" + spec["local_version"] + "-" + spec["wheel_tag"] + ".whl")
    args.output_directory.mkdir(parents=True, exist_ok=True)
    with output.open("xb") as destination:
        destination.write(result.getvalue())
    print(json.dumps({"filename": output.name, "sha256": sha256(result.getvalue()),
                      "upstream_sha256": spec["upstream_wheel_sha256"],
                      "unchanged_python_files": len(sources)-len(spec["source_patches"]),
                      "python_sources_modified": len(spec["source_patches"]),
                      "native_bridge_bytes_and_ABI_tag_preserved": True, "vendor_code_executed": False,
                      "runtime_qualified": False}))


if __name__ == "__main__":
    main()
