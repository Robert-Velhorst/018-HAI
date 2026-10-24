"""Internal-only Guardrails AI runner for fixed action-proposal validation."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hashlib
from importlib.metadata import version
import json
import re
from typing import Literal

from guardrails import Guard
from pydantic import BaseModel, ConfigDict, Field


MAX_REQUEST_BYTES = 16 * 1024
MAX_PROPOSAL_CHARS = 4096
MAX_REFERENCE_LENGTH = 96
REFERENCE_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$")
SCHEMA_NAME = "action_proposal"


class RequestError(ValueError):
    """Raised when the narrow validation boundary is not respected."""


class ActionProposal(BaseModel):
    """The only contract HAI may send to this local validation runner."""

    model_config = ConfigDict(extra="forbid")

    title: str = Field(min_length=1, max_length=120)
    summary: str = Field(min_length=1, max_length=600)
    risk: Literal["low", "medium", "high"]
    requiresApproval: bool
    nextAction: str = Field(min_length=1, max_length=200)
    sourceRefs: list[str] = Field(default_factory=list, max_length=10)


def new_action_guard():
    # Guardrails retains call inputs in instance history. Keep it request-local.
    guard = Guard.for_pydantic(output_class=ActionProposal)
    guard.configure(num_reasks=0, allow_metrics_collection=False)
    return guard


ENGINE = f"guardrails-ai {version('guardrails-ai')}"


def validate(payload: dict) -> dict:
    if not isinstance(payload, dict):
        raise RequestError("request must be an object")
    if payload.get("schema") != SCHEMA_NAME:
        raise RequestError("schema must be action_proposal")
    proposal = payload.get("proposal")
    if not isinstance(proposal, str) or not proposal or len(proposal) > MAX_PROPOSAL_CHARS:
        raise RequestError("proposal must be non-empty JSON text within the bounded limit")

    # Guardrails provides the Pydantic schema validation. Opaque source IDs are
    # constrained separately because source records must never be placed here.
    guard = new_action_guard()
    outcome = guard.validate(proposal, num_reasks=0)
    validated = outcome.validated_output
    if isinstance(validated, dict):
        for source_ref in validated.get("sourceRefs", []):
            if not isinstance(source_ref, str) or not REFERENCE_PATTERN.fullmatch(source_ref):
                raise RequestError("sourceRefs must contain only opaque identifiers")

    valid = bool(outcome.validation_passed)
    summaries = getattr(outcome, "validation_summaries", []) or []
    digest = hashlib.sha256(proposal.encode("utf-8")).hexdigest()
    return {
        "status": "valid" if valid else "needs_review",
        "engine": ENGINE,
        "schema": SCHEMA_NAME,
        "valid": valid,
        "violationCount": min(len(summaries), 20),
        "proposalDigest": digest,
        "scope": "Offline local Guardrails AI validation of one bounded action proposal. No proposal text or field values are returned, stored, exported, retried with an LLM, or used to authorize execution.",
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "HAI-Guardrails/1.0"

    def do_GET(self):
        if self.path != "/healthz":
            self.respond(404, {"error": "not found"})
            return
        self.respond(200, {"status": "ok", "engine": ENGINE, "scope": "internal fixed-schema validator"})

    def do_POST(self):
        if self.path != "/v1/validate":
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
            self.respond(200, validate(json.loads(self.rfile.read(length))))
        except (UnicodeDecodeError, json.JSONDecodeError, RequestError) as exc:
            self.respond(400, {"error": str(exc)})
        except Exception:
            self.respond(500, {"error": "local schema validation failed"})

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
