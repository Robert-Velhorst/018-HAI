"""Fetch only the pinned public source; do not import or execute vendor code."""
from pathlib import Path
import hashlib
import json
import shutil
import sys
from urllib.parse import urlparse
from urllib.request import HTTPRedirectHandler, build_opener


class PublicRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        public_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def public_url(url):
    parsed = urlparse(url)
    if (parsed.scheme != "https" or parsed.hostname not in
            {"www.kernel.org", "cdn.kernel.org", "mirrors.edge.kernel.org"}
            or parsed.username or parsed.password or parsed.port not in {None, 443}):
        raise ValueError("Source URL leaves the reviewed public publisher")


def main():
    if len(sys.argv) != 2:
        raise ValueError("Provide the dedicated public-input directory")
    spec_path = Path(__file__).with_name("sources.json")
    spec = json.loads(spec_path.read_text(encoding="utf-8"))
    output = Path(sys.argv[1]).resolve()
    output.mkdir(parents=True, exist_ok=True)
    public_url(spec["archive_url"])
    with build_opener(PublicRedirect()).open(spec["archive_url"], timeout=60) as response:
        public_url(response.url)
        raw = response.read(32 * 1024 * 1024 + 1)
    if len(raw) > 32 * 1024 * 1024 or hashlib.sha256(raw).hexdigest() != spec["archive_sha256"]:
        raise ValueError("Public source archive identity differs")
    with (output / spec["archive_name"]).open("xb") as destination:
        destination.write(raw)
    for name in ["sources.json", "build_source.py"]:
        shutil.copyfile(spec_path.with_name(name), output / name)


if __name__ == "__main__":
    main()
