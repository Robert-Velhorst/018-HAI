"""Build a source-bound Dynaconf profile without importing vendor code."""
from pathlib import Path, PurePosixPath
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener
import argparse
import ast
import base64
import csv
import email
import hashlib
import io
import json
import zipfile


def digest(data):
    return hashlib.sha256(data).hexdigest()


def check_url(value):
    parsed = urlsplit(value)
    if (parsed.scheme != "https" or parsed.hostname != "files.pythonhosted.org"
            or parsed.username or parsed.password or parsed.port not in {None, 443}):
        raise ValueError("Unapproved upstream wheel source")


class Redirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        check_url(newurl)
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
        check_url(spec["upstream_url"])
        with build_opener(Redirects()).open(spec["upstream_url"], timeout=60) as response:
            check_url(response.url)
            raw = response.read(8 * 1024 * 1024 + 1)
    if len(raw) > 8 * 1024 * 1024 or digest(raw) != spec["upstream_wheel_sha256"]:
        raise ValueError("Upstream source wheel differs")
    files = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist()) > 2000 or sum(v.file_size for v in archive.infolist()) > 32 * 1024 * 1024:
            raise ValueError("Unexpected wheel bounds")
        for member in archive.infolist():
            path = PurePosixPath(member.filename)
            if path.is_absolute() or ".." in path.parts or "\\" in member.filename:
                raise ValueError("Unexpected wheel member")
            if not member.is_dir():
                if member.filename in files:
                    raise ValueError("Duplicate wheel member")
                files[member.filename] = archive.read(member)
    old_info = "dynaconf-" + spec["upstream_version"] + ".dist-info"
    new_info = "dynaconf-" + spec["local_version"] + ".dist-info"
    original_metadata = email.message_from_bytes(files[old_info + "/METADATA"])
    if original_metadata["Name"] != "dynaconf" or original_metadata["Version"] != spec["upstream_version"]:
        raise ValueError("Unexpected upstream package metadata")
    retired = {n: digest(v) for n, v in files.items() if n.startswith("dynaconf/vendor/")}
    if retired != spec["retired_vendor_files"]:
        raise ValueError("Bundled vendor source differs from the review")
    for name in retired:
        del files[name]
    for name, adapter in spec["adapter_files"].items():
        value = adapter["source"].encode("utf-8")
        if digest(value) != adapter["sha256"] or not name.startswith("dynaconf/vendor/"):
            raise ValueError("Adapter identity differs")
        ast.parse(value)
        files[name] = value
    for name, expected in spec["unchanged_core_modules"].items():
        if digest(files[name]) != expected:
            raise ValueError("Dynaconf core module changed: " + name)
    files["dynaconf/VERSION"] = (spec["local_version"] + "\n").encode()
    header, separator, body = files[old_info + "/METADATA"].replace(b"\r\n", b"\n").partition(b"\n\n")
    if not separator:
        raise ValueError("Invalid metadata envelope")
    header = header.replace(("Version: " + spec["upstream_version"] + "\n").encode(),
                            ("Version: " + spec["local_version"] + "\n").encode(), 1)
    lines = header.splitlines()
    python_rows = [i for i, line in enumerate(lines) if line.startswith(b"Requires-Python:")]
    if len(python_rows) != 1:
        raise ValueError("Unexpected Python metadata")
    lines[python_rows[0]] = ("Requires-Python: " + spec["requires_python"]).encode()
    lines = [line for line in lines if not line.startswith(b"Classifier: Programming Language :: Python :: 3.")]
    lines.append(b"Classifier: Programming Language :: Python :: 3.15")
    lines += [("Requires-Dist: " + requirement).encode() for requirement in spec["external_requirements"]]
    files[old_info + "/METADATA"] = b"\n".join(lines) + b"\n\n" + body + ("\n\nHAI modified profile: " + spec["profile"] + "\n").encode()
    wheel_lines = files[old_info + "/WHEEL"].decode().splitlines()
    wheel_lines = ["Generator: HAI source-bound Dynaconf profile builder" if line.startswith("Generator:") else line for line in wheel_lines]
    files[old_info + "/WHEEL"] = ("\n".join(wheel_lines) + "\n").encode()
    del files[old_info + "/RECORD"]
    files = {name.replace(old_info + "/", new_info + "/", 1): value for name, value in files.items()}
    manifest = {k: v for k, v in spec.items() if k != "adapter_files"}
    manifest["adapter_files"] = {name: {"sha256": value["sha256"]} for name, value in spec["adapter_files"].items()}
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
    name = "dynaconf-" + spec["local_version"] + "-py3-none-any.whl"
    with (args.output_directory / name).open("xb") as output:
        output.write(stream.getvalue())
    print(json.dumps({"filename": name, "sha256": digest(stream.getvalue()),
                      "upstream_sha256": spec["upstream_wheel_sha256"],
                      "unchanged_core_modules": len(spec["unchanged_core_modules"]),
                      "retired_vendor_files": len(retired), "vendor_code_executed": False,
                      "runtime_qualified": False}))


if __name__ == "__main__":
    main()
