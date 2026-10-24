"""Internal-only Evidently report runner for bounded, non-sensitive fixtures."""

from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hashlib
import json
import os

# This bounded runner is offline; disable SDK telemetry before its imports.
os.environ["DO_NOT_TRACK"] = "1"

import evidently
import pandas as pd
from evidently import Report
from evidently.presets import DataSummaryPreset


MAX_REQUEST_BYTES = 64 * 1024
MAX_CASES = 25
MAX_ID_LENGTH = 96
MAX_INPUT_LENGTH = 512
MAX_OUTPUT_LENGTH = 2048
ALLOWED_KINDS = {"synthetic", "redacted"}


class RequestError(ValueError):
    """Raised when an evaluation fixture exceeds the narrow report contract."""


def evaluate(payload: dict) -> dict:
    if not isinstance(payload, dict):
        raise RequestError("request must be an object")
    fixture_kind = payload.get("fixtureKind")
    if fixture_kind not in ALLOWED_KINDS:
        raise RequestError("fixtureKind must be synthetic or redacted")
    raw_cases = payload.get("cases")
    if not isinstance(raw_cases, list) or not raw_cases or len(raw_cases) > MAX_CASES:
        raise RequestError(f"cases must contain 1 to {MAX_CASES} items")

    cases = []
    seen = set()
    for raw_case in raw_cases:
        if not isinstance(raw_case, dict):
            raise RequestError("every case must be an object")
        case_id = raw_case.get("id")
        if not isinstance(case_id, str) or not safe_id(case_id) or case_id in seen:
            raise RequestError("case ids must be unique opaque identifiers")
        seen.add(case_id)
        input_text = bounded_string(raw_case.get("input"), "input", MAX_INPUT_LENGTH)
        output_text = bounded_string(raw_case.get("output"), "output", MAX_OUTPUT_LENGTH)
        cases.append({"id": case_id, "input": input_text, "output": output_text})

    # Run an actual offline Evidently report. The response deliberately omits
    # the report body because that body can contain fixture-derived values.
    frame = pd.DataFrame(cases, columns=["id", "input", "output"])
    report = Report([DataSummaryPreset()])
    report.run(current_data=frame)

    output_values = [item["output"] for item in cases]
    non_empty = [value for value in output_values if value]
    counts = Counter(non_empty)
    duplicate_outputs = sum(count - 1 for count in counts.values() if count > 1)
    output_lengths = [len(value) for value in output_values]
    digest = hashlib.sha256(
        json.dumps(
            {"fixtureKind": fixture_kind, "cases": cases},
            separators=(",", ":"),
            sort_keys=True,
        ).encode("utf-8")
    ).hexdigest()
    empty_outputs = len(output_values) - len(non_empty)
    return {
        "status": "passed" if empty_outputs == 0 else "needs_review",
        "engine": f"evidently {evidently.__version__}",
        "fixtureKind": fixture_kind,
        "caseCount": len(cases),
        "emptyOutputs": empty_outputs,
        "duplicateOutputs": duplicate_outputs,
        "averageOutputChars": round(sum(output_lengths) / len(output_lengths), 2),
        "reportDigest": digest,
        "scope": "Offline local Evidently DataSummary report over bounded synthetic or redacted fixtures. No fixture text is returned, stored, exported, or used to change routing, policy, completion, or execution.",
    }


def bounded_string(value, field: str, limit: int) -> str:
    if not isinstance(value, str) or len(value) > limit:
        raise RequestError(f"{field} must be a string at most {limit} characters")
    return value


def safe_id(value: str) -> bool:
    return bool(value) and len(value) <= MAX_ID_LENGTH and all(
        ("a" <= char <= "z") or ("A" <= char <= "Z") or ("0" <= char <= "9") or char in "_-"
        for char in value
    )


class Handler(BaseHTTPRequestHandler):
    server_version = "HAI-Evidently/1.0"

    def do_GET(self):
        if self.path != "/healthz":
            self.respond(404, {"error": "not found"})
            return
        self.respond(200, {"status": "ok", "engine": f"evidently {evidently.__version__}", "scope": "internal bounded report runner"})

    def do_POST(self):
        if self.path != "/v1/evaluate":
            self.respond(404, {"error": "not found"})
            return
        if self.headers.get("Content-Type", "").split(";", 1)[0].strip().lower() != "application/json":
            self.respond(415, {"error": "application/json required"})
            return
        try:
            length = int(self.headers.get("Content-Length", ""))
        except ValueError:
            self.respond(411, {"error": "content length required"})
            return
        if length < 1 or length > MAX_REQUEST_BYTES:
            self.respond(413, {"error": "request size outside bounded limit"})
            return
        try:
            self.respond(200, evaluate(json.loads(self.rfile.read(length))))
        except (UnicodeDecodeError, json.JSONDecodeError, RequestError) as exc:
            self.respond(400, {"error": str(exc)})
        except Exception:
            self.respond(500, {"error": "local evaluation failed"})

    def respond(self, status: int, payload: dict):
        data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, _format, *_args):
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
