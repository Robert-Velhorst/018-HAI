"""Fetch only hash-bound public ABI transition inputs; never execute source."""
from pathlib import Path
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener
import hashlib
import json
import shutil
import sys


def main():
    if len(sys.argv) != 2:
        raise ValueError('Provide the dedicated public input directory')
    folder = Path(__file__).resolve().parent
    spec = json.loads((folder/'abi-sources.json').read_text())
    items = [item for source in spec['sources'] for item in source['inputs']]
    allowed = {item['url'] for item in items}

    def validate(url):
        parsed = urlsplit(url)
        if (url not in allowed or parsed.scheme != 'https' or parsed.hostname != 'deb.debian.org'
                or parsed.username or parsed.password or parsed.port not in {None, 443}
                or parsed.query or parsed.fragment):
            raise ValueError('Unexpected ABI source URL')

    class Redirect(HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            validate(newurl)
            return super().redirect_request(req, fp, code, msg, headers, newurl)

    output = Path(sys.argv[1]).resolve()
    output.mkdir(parents=True, exist_ok=True)
    for item in items:
        if Path(item['name']).name != item['name']:
            raise ValueError('Unexpected source filename')
        validate(item['url'])
        with build_opener(Redirect()).open(item['url'], timeout=60) as response:
            validate(response.url)
            raw = response.read(item['bytes']+1)
        if len(raw) != item['bytes'] or hashlib.sha256(raw).hexdigest() != item['sha256']:
            raise ValueError('Published ABI source identity differs')
        with (output/item['name']).open('xb') as handle:
            handle.write(raw)
    shutil.copyfile(folder/'abi-sources.json', output/'abi-sources.json')
    shutil.copyfile(folder/'build_abi_packages.py', output/'build_abi_packages.py')


if __name__ == '__main__':
    main()
