"""Fetch reviewed public Debian source inputs; do not execute them."""
from pathlib import Path
import hashlib,json,shutil,sys
from urllib.parse import urlparse
from urllib.request import HTTPRedirectHandler,build_opener

def public_url(url):
    p=urlparse(url)
    if p.scheme!='https' or p.hostname!='deb.debian.org' or p.username or p.password or p.port not in {None,443} or not p.path.startswith('/debian/pool/main/a/acl/'):
        raise ValueError('Source URL leaves the reviewed public Debian directory')
class PublicRedirect(HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        public_url(newurl)
        return super().redirect_request(req,fp,code,msg,headers,newurl)
def main():
    if len(sys.argv)!=2:raise ValueError('Provide the dedicated public input directory')
    folder=Path(__file__).resolve().parent;spec=json.loads((folder/'sources.json').read_text(encoding='utf-8'))
    output=Path(sys.argv[1]).resolve();output.mkdir(parents=True,exist_ok=True)
    for item in spec['inputs']:
        if Path(item['name']).name!=item['name']:raise ValueError('Unexpected input path')
        public_url(item['url'])
        with build_opener(PublicRedirect()).open(item['url'],timeout=60) as response:
            public_url(response.url);raw=response.read(4*1024*1024+1)
        if len(raw)!=item['bytes'] or hashlib.sha256(raw).hexdigest()!=item['sha256']:raise ValueError('Public source input identity differs')
        with (output/item['name']).open('xb') as handle:handle.write(raw)
    for name in ['sources.json','build_packages.py']:shutil.copyfile(folder/name,output/name)
if __name__=='__main__':main()
