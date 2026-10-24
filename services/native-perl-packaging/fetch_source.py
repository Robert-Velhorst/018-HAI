"""Fetch reviewed public Debian Perl source inputs; do not execute them."""
from pathlib import Path
import hashlib,html,json,re,shutil,sys
from urllib.parse import parse_qs,urlparse
from urllib.request import HTTPRedirectHandler,build_opener

def public_url(url):
    p=urlparse(url)
    distribution=p.hostname=='deb.debian.org' and p.path.startswith('/debian/pool/main/p/perl/') and not p.query
    certificate=p.hostname=='keyring.debian.org' and p.path=='/pks/lookup' and parse_qs(p.query)=={'op':['get'],'search':['0x76A28E42C9811D91E88FBA5E2EC0FFB3B7301B1F']}
    if p.scheme!='https' or p.username or p.password or p.port not in {None,443} or not (distribution or certificate):
        raise ValueError('Source URL leaves the reviewed Debian inputs or exact existing-key lookup')
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
            public_url(response.url);raw=response.read(32*1024*1024+1)
        if item.get('format')=='public-key-armor-from-HTML':
            if len(raw)>1024*1024:raise ValueError('Unexpected certificate response size')
            blocks=re.findall(r'-----BEGIN PGP PUBLIC KEY BLOCK-----.*?-----END PGP PUBLIC KEY BLOCK-----',html.unescape(raw.decode('latin-1')),re.S)
            if len(blocks)!=1:raise ValueError('Expected one public certificate')
            raw=(blocks[0].replace('\r\n','\n')+'\n').encode('ascii')
            if b'PRIVATE KEY' in raw or b'<' in raw:raise ValueError('Unexpected public certificate encoding')
        if len(raw)!=item['bytes'] or hashlib.sha256(raw).hexdigest()!=item['sha256']:raise ValueError('Public source input identity differs')
        with (output/item['name']).open('xb') as handle:handle.write(raw)
    for name in ['sources.json','build_packages.py']:shutil.copyfile(folder/name,output/name)
if __name__=='__main__':main()
