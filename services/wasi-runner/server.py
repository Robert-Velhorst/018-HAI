import hashlib, json, os, subprocess
from http.server import BaseHTTPRequestHandler, HTTPServer

TOKEN=os.environ.get("HAI_WASI_RUNNER_TOKEN", "")
ROOT="/modules"

def reply(handler, code, body):
    data=json.dumps(body).encode(); handler.send_response(code); handler.send_header("Content-Type","application/json"); handler.send_header("Content-Length",str(len(data))); handler.end_headers(); handler.wfile.write(data)

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        if self.path == "/healthz": return reply(self,200,{"status":"ok","runtime":"wasmtime 50.0.0","scope":"no preopens, no arguments, no environment"})
        return reply(self,404,{"error":"not found"})
    def do_POST(self):
        if self.path != "/run" or not TOKEN or self.headers.get("X-HAI-WASI-Token") != TOKEN: return reply(self,404,{"error":"not found"})
        try:
            length=int(self.headers.get("Content-Length","0")); raw=self.rfile.read(min(length,4096)); module=json.loads(raw)
            name=module["file"]; expected=module["sha256"].lower()
            if not name.endswith(".wasm") or os.path.basename(name)!=name or len(expected)!=64: raise ValueError()
            path=os.path.join(ROOT,name)
            with open(path,"rb") as source: actual=hashlib.sha256(source.read()).hexdigest()
            if actual != expected: return reply(self,200,{"status":"failed","summary":"module digest does not match its HAI admission manifest","exitCode":-1})
            result=subprocess.run(["timeout","5s","wasmtime","run",path],stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,env={"PATH":"/usr/local/bin:/usr/bin:/bin","XDG_CACHE_HOME":"/tmp/cache","WASMTIME_WASI_PREVIEW0":"true","WASMTIME_WASI_INHERIT_ENV":"false"},cwd="/tmp",timeout=6)
            return reply(self,200,{"status":"completed" if result.returncode==0 else "failed","summary":"manifest-approved WASI module completed without host capabilities" if result.returncode==0 else "manifest-approved WASI module failed within the resource boundary","exitCode":result.returncode})
        except Exception: return reply(self,200,{"status":"failed","summary":"WASI module could not be admitted or executed","exitCode":-1})

HTTPServer(("0.0.0.0",8080),Handler).serve_forever()
