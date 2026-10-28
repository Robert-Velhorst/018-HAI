"""Bounded, local-only Garak prompt-injection regression runner for HAI."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import version
from pathlib import Path
from tempfile import TemporaryDirectory
from threading import Lock
from urllib.parse import urlparse
from urllib.error import HTTPError
from urllib.request import HTTPRedirectHandler, Request as URLRequest, build_opener
import hashlib
import ipaddress
import json
import os
import subprocess
import time


SUITE = "hai_prompt_injection_regression_v1"
PROBE = "promptinject.HijackLongPrompt"
DETECTOR = "promptinject.AttackRogueString"
MAX_CASES = 4
MAX_RUN_SECONDS = 150
MAX_REQUEST_BYTES = 256
ALLOWED_HOSTS = {
    "localhost",
    "host.docker.internal",
    "ollama",
    "localai",
    "vllm",
    "llama-cpp",
    "mistral-rs",
}
RUN_LOCK = Lock()


class RequestError(ValueError):
    """Raised when configuration or a caller crosses the fixed-suite boundary."""


class RejectRedirects(HTTPRedirectHandler):
    def redirect_request(self, _request, _response, _code, _message, _headers, _new_url):
        return None


def engine_name() -> str:
    return f"garak {version('garak')}"


def validate_local_url(raw: str) -> str:
    value = raw.strip().rstrip("/")
    parsed = urlparse(value)
    if not value or parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise RequestError("local model URL must be a plain local HTTP(S) URL")
    host = parsed.hostname.lower()
    try:
        address = ipaddress.ip_address(host)
        if not (address.is_loopback or address.is_private):
            raise RequestError("local model URL must use a loopback or private-network IP")
    except ValueError:
        if host not in ALLOWED_HOSTS:
            raise RequestError("local model URL host is not allowlisted")
    return value


def configuration(environment: dict | None = None) -> tuple[str, str, str]:
    environment = environment or os.environ
    base_url = validate_local_url(environment.get("HAI_GARAK_MODEL_BASE_URL", ""))
    model_id = environment.get("HAI_GARAK_MODEL_ID", "").strip()
    api_key = environment.get("HAI_GARAK_MODEL_API_KEY", "").strip() or "local-no-key"
    if not model_id or len(model_id) > 96 or any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:/-" for character in model_id):
        raise RequestError("a bounded HAI_GARAK_MODEL_ID is required")
    return base_url, model_id, api_key


def probe() -> dict:
    base_url, model_id, api_key = configuration()
    request = URLRequest(base_url + "/models", headers={"Accept": "application/json", "Authorization": f"Bearer {api_key}"})
    try:
        with build_opener(RejectRedirects()).open(request, timeout=5) as response:
            payload = json.loads(response.read(32 * 1024))
    except HTTPError as exc:
        if 300 <= exc.code < 400:
            exc.close()
            raise RequestError("local model redirects are not allowed") from None
        raise
    models = payload.get("data", []) if isinstance(payload, dict) else []
    if not any(isinstance(item, dict) and item.get("id") == model_id for item in models):
        raise RequestError("configured local model is not reported by its endpoint")
    return {"status": "ok", "engine": engine_name(), "configured": True, "modelId": model_id, "suite": SUITE}


def garak_environment(api_key: str) -> dict:
    # Do not inherit provider credentials or proxy settings. Garak can only see
    # the explicit local OpenAI-compatible credential supplied for this run.
    return {
        "PATH": os.environ.get("PATH", "/usr/local/bin:/usr/bin:/bin"),
        "HOME": "/tmp",
        "XDG_CONFIG_HOME": "/tmp/.config",
        "XDG_DATA_HOME": "/tmp/.local/share",
        "XDG_CACHE_HOME": "/tmp/.cache",
        "PYTHONDONTWRITEBYTECODE": "1",
        "OPENAICOMPATIBLE_API_KEY": api_key,
        "HTTP_PROXY": "",
        "HTTPS_PROXY": "",
        "ALL_PROXY": "",
        "http_proxy": "",
        "https_proxy": "",
        "all_proxy": "",
        "NO_PROXY": "*",
        "no_proxy": "*",
    }


def run_config(base_url: str) -> dict:
    return {
        "system": {"parallel_requests": False, "parallel_attempts": False, "lite": True},
        "run": {"seed": 20260720, "generations": 1, "soft_probe_prompt_cap": MAX_CASES},
        "plugins": {"generators": {"hai_local": {"LocalOpenAICompatible": {"uri": base_url, "temperature": 0, "top_p": 1.0, "max_tokens": 64}}}},
        "reporting": {"report_dir": "/tmp/garak-runs", "bootstrap_num_iterations": 1, "confidence_interval_method": "none"},
    }


def parse_report(path: Path, model_id: str, duration_ms: int) -> dict:
    evaluations = []
    with path.open("r", encoding="utf-8") as report:
        for line in report:
            record = json.loads(line)
            if record.get("entry_type") == "eval":
                evaluations.append(record)
    if len(evaluations) != 1:
        raise RequestError("fixed Garak suite returned an unexpected evaluation count")
    result = evaluations[0]
    passed = result.get("passed")
    failed = result.get("fails")
    case_count = result.get("total_evaluated")
    if result.get("probe") != PROBE or result.get("detector") != DETECTOR or not isinstance(passed, int) or not isinstance(failed, int) or not isinstance(case_count, int) or not 1 <= case_count <= MAX_CASES or passed < 0 or failed < 0 or passed + failed != case_count:
        raise RequestError("fixed Garak suite returned invalid aggregate metadata")
    metadata = {"suite": SUITE, "modelId": model_id, "probe": PROBE, "detector": DETECTOR, "caseCount": case_count, "passedCount": passed, "failedCount": failed, "score": passed / case_count}
    return {
        "status": "completed",
        "engine": engine_name(),
        **metadata,
        "durationMs": duration_ms,
        "resultDigest": hashlib.sha256(json.dumps(metadata, sort_keys=True, separators=(",", ":")).encode("utf-8")).hexdigest(),
        "scope": "Aggregate result from a fixed four-case Garak prompt-injection scan only. No HAI workflow, connected source, credential, runtime, action, raw prompt, model output, or full report is returned, retained, or exported. This is regression evidence only and cannot authorize, route, or execute work.",
    }


def run_scan() -> dict:
    base_url, model_id, api_key = configuration()
    started = time.monotonic()
    with TemporaryDirectory(prefix="hai-garak-", dir="/tmp") as directory:
        workdir = Path(directory)
        config_path = workdir / "fixed-suite.json"
        report_prefix = workdir / "fixed-scan"
        config_path.write_text(json.dumps(run_config(base_url)), encoding="utf-8")
        command = [
            "garak",
            "--target_type", "hai_local.LocalOpenAICompatible",
            "--target_name", model_id,
            "--probes", PROBE,
            "--generations", "1",
            "--config", str(config_path),
            "--report_prefix", str(report_prefix),
        ]
        try:
            completed = subprocess.run(command, cwd=workdir, env=garak_environment(api_key), stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=MAX_RUN_SECONDS, check=False)
        except subprocess.TimeoutExpired as exc:
            raise RequestError("fixed Garak suite exceeded its time limit") from exc
        if completed.returncode != 0:
            raise RequestError("fixed Garak suite could not complete with the configured local model")
        report_path = Path(f"{report_prefix}.report.jsonl")
        if not report_path.is_file():
            raise RequestError("fixed Garak suite did not produce its aggregate report")
        result = parse_report(report_path, model_id, round((time.monotonic() - started) * 1000))
        # TemporaryDirectory removes Garak JSONL, hit logs, and HTML output.
        return result


def response(handler: BaseHTTPRequestHandler, status: int, payload: dict) -> None:
    data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Cache-Control", "no-store")
    handler.send_header("Content-Length", str(len(data)))
    handler.end_headers()
    handler.wfile.write(data)


class Handler(BaseHTTPRequestHandler):
    server_version = "HAI-Garak/1.0"

    def do_GET(self):
        if self.path != "/healthz":
            response(self, 404, {"error": "not found"})
            return
        try:
            _, model_id, _ = configuration()
            response(self, 200, {"status": "ok", "engine": engine_name(), "configured": True, "modelId": model_id, "suite": SUITE})
        except RequestError:
            response(self, 200, {"status": "ok", "engine": engine_name(), "configured": False, "suite": SUITE, "scope": "runner is healthy; local model is not configured"})

    def do_POST(self):
        if self.path == "/v1/probe":
            self.handle_probe()
        elif self.path == "/v1/run":
            self.handle_run()
        else:
            response(self, 404, {"error": "not found"})

    def handle_probe(self):
        if self.headers.get("Content-Length") not in {None, "0"}:
            response(self, 413, {"error": "probe accepts no input"})
            return
        try:
            response(self, 200, probe())
        except RequestError as exc:
            response(self, 400, {"error": str(exc)})
        except Exception:
            response(self, 502, {"error": "local model endpoint could not be verified"})

    def handle_run(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            response(self, 400, {"error": "invalid content length"})
            return
        if length > MAX_REQUEST_BYTES:
            response(self, 413, {"error": "request is too large"})
            return
        raw = self.rfile.read(length) if length else b"{}"
        if raw != b"{}":
            response(self, 400, {"error": "runner accepts no caller-provided target, model, endpoint, prompt, probe, command, or data"})
            return
        if not RUN_LOCK.acquire(blocking=False):
            response(self, 409, {"error": "a fixed Garak scan is already running"})
            return
        try:
            response(self, 200, run_scan())
        except RequestError as exc:
            response(self, 400, {"error": str(exc)})
        except Exception:
            response(self, 502, {"error": "fixed Garak scan could not complete with the configured local model"})
        finally:
            RUN_LOCK.release()

    def log_message(self, _format, *_args):
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
