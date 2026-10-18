"""Bounded local DeepEval source-grounding regression runner for HAI."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import version
from threading import Lock
from urllib.parse import urlparse
from urllib.request import ProxyHandler, Request as URLRequest, build_opener
import asyncio
import hashlib
import ipaddress
import json
import os
import time

from deepeval.metrics import FaithfulnessMetric
from deepeval.models import DeepEvalBaseLLM
from deepeval.test_case import LLMTestCase


SUITE = "hai_source_grounding_regression_v1"
METRIC = "FaithfulnessMetric"
MAX_CASES = 3
MAX_REQUEST_BYTES = 256
MAX_RUN_SECONDS = 180
MAX_MODEL_RESPONSE_BYTES = 64 * 1024
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

# These deliberately contain no HAI, customer, account, or connected-source data.
CASES = (
    {
        "input": "What is the filing deadline?",
        "context": "The filing deadline is 9 September 2026. Submit the evidence bundle before that date.",
        "answer": "The filing deadline is 9 September 2026.",
        "faithful": True,
    },
    {
        "input": "What is HAI's paid-model budget?",
        "context": "HAI keeps paid calls disabled by default and the daily paid budget is EUR 0.",
        "answer": "HAI's daily paid-model budget is EUR 0 and paid calls are disabled by default.",
        "faithful": True,
    },
    {
        "input": "Can HAI send a legal email automatically?",
        "context": "Legal emails require human approval before HAI can send them.",
        "answer": "HAI can send legal emails automatically without approval.",
        "faithful": False,
    },
)


class RequestError(ValueError):
    """Raised when configuration or a caller crosses the fixed-suite boundary."""


def engine_name() -> str:
    return f"deepeval {version('deepeval')}"


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
    base_url = validate_local_url(environment.get("HAI_DEEPEVAL_MODEL_BASE_URL", ""))
    model_id = environment.get("HAI_DEEPEVAL_MODEL_ID", "").strip()
    api_key = environment.get("HAI_DEEPEVAL_MODEL_API_KEY", "").strip() or "local-no-key"
    if not model_id or len(model_id) > 96 or any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:/-" for character in model_id):
        raise RequestError("a bounded HAI_DEEPEVAL_MODEL_ID is required")
    return base_url, model_id, api_key


class LocalOpenAIJudge(DeepEvalBaseLLM):
    def __init__(self, base_url: str, model_id: str, api_key: str):
        self.base_url = base_url
        self.model_id = model_id
        self.api_key = api_key
        self.opener = build_opener(ProxyHandler({}))
        super().__init__(model=model_id)

    def load_model(self):
        return None

    def generate(self, prompt, schema=None):
        payload = {
            "model": self.model_id,
            "messages": [{"role": "user", "content": str(prompt)}],
            "temperature": 0,
            "max_tokens": 320,
        }
        request = URLRequest(
            self.base_url + "/chat/completions",
            data=json.dumps(payload, separators=(",", ":")).encode("utf-8"),
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self.api_key}",
                "User-Agent": "HAI-DeepEval/1.0",
            },
            method="POST",
        )
        with self.opener.open(request, timeout=25) as response:  # nosec B310: base URL is local-only validated.
            if response.status != 200:
                raise RequestError("local model returned an unsuccessful response")
            body = response.read(MAX_MODEL_RESPONSE_BYTES + 1)
        if len(body) > MAX_MODEL_RESPONSE_BYTES:
            raise RequestError("local model response exceeds the safety limit")
        try:
            payload = json.loads(body)
            content = payload["choices"][0]["message"]["content"]
        except (KeyError, TypeError, ValueError, IndexError) as exc:
            raise RequestError("local model returned invalid completion data") from exc
        if not isinstance(content, str) or not content.strip() or len(content) > MAX_MODEL_RESPONSE_BYTES:
            raise RequestError("local model returned invalid completion content")
        return content

    async def a_generate(self, *args, **kwargs):
        return await asyncio.to_thread(self.generate, *args, **kwargs)

    def get_model_name(self):
        return self.model_id


def probe() -> dict:
    base_url, model_id, api_key = configuration()
    request = URLRequest(base_url + "/models", headers={"Accept": "application/json", "Authorization": f"Bearer {api_key}"})
    with build_opener(ProxyHandler({})).open(request, timeout=5) as response:  # nosec B310: base URL is local-only validated.
        payload = json.loads(response.read(32 * 1024))
    models = payload.get("data", []) if isinstance(payload, dict) else []
    if not any(isinstance(item, dict) and item.get("id") == model_id for item in models):
        raise RequestError("configured local model is not reported by its endpoint")
    return {"status": "ok", "engine": engine_name(), "configured": True, "modelId": model_id, "suite": SUITE, "metric": METRIC}


def run_evaluation() -> dict:
    base_url, model_id, api_key = configuration()
    started = time.monotonic()
    judge = LocalOpenAIJudge(base_url, model_id, api_key)
    passed_count = 0
    scores = []
    for case in CASES:
        metric = FaithfulnessMetric(
            threshold=0.85,
            model=judge,
            include_reason=False,
            async_mode=False,
            strict_mode=False,
            verbose_mode=False,
            truths_extraction_limit=4,
            penalize_ambiguous_claims=True,
        )
        score = metric.measure(
            LLMTestCase(input=case["input"], actual_output=case["answer"], retrieval_context=[case["context"]]),
            _show_indicator=False,
        )
        if not isinstance(score, (int, float)) or not 0 <= score <= 1:
            raise RequestError("fixed DeepEval suite returned an invalid score")
        scores.append(float(score))
        judged_faithful = score >= 0.85
        if judged_faithful == case["faithful"]:
            passed_count += 1
    metadata = {
        "suite": SUITE,
        "metric": METRIC,
        "modelId": model_id,
        "caseCount": len(CASES),
        "passedCount": passed_count,
        "failedCount": len(CASES) - passed_count,
        "score": sum(scores) / len(scores),
    }
    return {
        "status": "completed",
        "engine": engine_name(),
        **metadata,
        "durationMs": round((time.monotonic() - started) * 1000),
        "resultDigest": hashlib.sha256(json.dumps(metadata, sort_keys=True, separators=(",", ":")).encode("utf-8")).hexdigest(),
        "scope": "Aggregate result from three fixed synthetic source-grounding cases only. No HAI answer, connected source, credential, workflow, runtime, action, raw fixture, model generation, metric reason, or case row is read, returned, retained, or exported. This is regression evidence only and cannot verify, route, approve, or execute work.",
    }


def response(handler: BaseHTTPRequestHandler, status: int, payload: dict) -> None:
    data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Cache-Control", "no-store")
    handler.send_header("Content-Length", str(len(data)))
    handler.end_headers()
    handler.wfile.write(data)


class Handler(BaseHTTPRequestHandler):
    server_version = "HAI-DeepEval/1.0"

    def do_GET(self):
        if self.path != "/healthz":
            response(self, 404, {"error": "not found"})
            return
        try:
            _, model_id, _ = configuration()
            response(self, 200, {"status": "ok", "engine": engine_name(), "configured": True, "modelId": model_id, "suite": SUITE, "metric": METRIC})
        except RequestError:
            response(self, 200, {"status": "ok", "engine": engine_name(), "configured": False, "suite": SUITE, "metric": METRIC, "scope": "runner is healthy; local model is not configured"})

    def do_POST(self):
        if self.path == "/v1/probe":
            self.handle_probe()
            return
        if self.path == "/v1/run":
            self.handle_run()
            return
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
            response(self, 400, {"error": "runner accepts no caller-provided answer, source, model, endpoint, prompt, metric, command, or data"})
            return
        if not RUN_LOCK.acquire(blocking=False):
            response(self, 409, {"error": "a fixed DeepEval evaluation is already running"})
            return
        try:
            response(self, 200, run_evaluation())
        except RequestError as exc:
            response(self, 400, {"error": str(exc)})
        except Exception:
            response(self, 502, {"error": "fixed DeepEval evaluation could not complete with the configured local model"})
        finally:
            RUN_LOCK.release()

    def log_message(self, _format, *_args):
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
