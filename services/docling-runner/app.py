"""Internal-only, offline Docling document extractor for HAI source intake.

The runner accepts exactly one pre-registered relative folder within /intake.
It does not accept uploads, arbitrary paths, models, OCR/table settings, parser
flags, network targets, or remote-service options. Office/HTML/text formats use
Docling's local conversion path. PDF mode is disabled unless an operator
pre-provisions Docling artifacts under the separate read-only /models mount.
"""
from __future__ import annotations

from hashlib import sha256
from hmac import compare_digest
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import PackageNotFoundError, version
import json
import os
from pathlib import Path, PurePosixPath
import re
from threading import Lock


# Force offline behavior before Docling or Hugging Face libraries are imported.
os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["TRANSFORMERS_OFFLINE"] = "1"
os.environ["HF_HUB_DISABLE_TELEMETRY"] = "1"
os.environ["DO_NOT_TRACK"] = "1"
os.environ["HTTP_PROXY"] = ""
os.environ["HTTPS_PROXY"] = ""
os.environ["ALL_PROXY"] = ""
os.environ["http_proxy"] = ""
os.environ["https_proxy"] = ""
os.environ["all_proxy"] = ""
os.environ["NO_PROXY"] = "*"
os.environ["no_proxy"] = "*"
os.environ.setdefault("OMP_NUM_THREADS", "2")
os.environ.setdefault("MKL_NUM_THREADS", "2")

INTAKE_ROOT = Path("/intake")
ARTIFACTS_ROOT = Path("/models")
MAX_REQUEST_BYTES = 1024
MAX_DOCUMENTS = 10
MAX_DOCUMENT_BYTES = 10 * 1024 * 1024
MAX_DOCUMENT_CHARS = 250_000
MAX_TOTAL_CHARS = 2_000_000
MAX_PAGES = 100
MAX_FILESYSTEM_ENTRIES = 2_000
TOKEN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{15,159}$")
RUN_LOCK = Lock()

TEXT_FORMATS = {".txt": "text", ".md": "markdown"}
DOCLING_FORMATS = {".docx": "docx", ".pptx": "pptx", ".xlsx": "xlsx", ".html": "html", ".htm": "html"}


class RequestError(ValueError):
    """The caller or local configuration crossed a fixed runner boundary."""


def bool_env(name: str, default: bool = False) -> bool:
    value = os.environ.get(name, str(default)).strip().lower()
    return value in {"1", "true", "yes", "on"}


def docling_version() -> str:
    try:
        value = version("docling")
    except PackageNotFoundError as exc:
        raise RequestError("Docling package is unavailable") from exc
    if not re.fullmatch(r"\d+(?:\.\d+){1,3}(?:[-+][A-Za-z0-9.-]+)?", value):
        raise RequestError("Docling returned an invalid version")
    return value


def configured() -> tuple[str, Path, bool, Path]:
    token = os.environ.get("HAI_DOCLING_RUNNER_TOKEN", "").strip()
    root = Path(os.environ.get("HAI_DOCLING_INPUT_ROOT", "/intake")).resolve()
    pdf_enabled = bool_env("HAI_DOCLING_PDF_ENABLED")
    artifacts = Path(os.environ.get("HAI_DOCLING_ARTIFACTS_ROOT", "/models")).resolve()
    if not TOKEN.fullmatch(token):
        raise RequestError("runner token must have at least 16 bounded characters")
    if not root.is_dir():
        raise RequestError("selected document input root is unavailable")
    if pdf_enabled and (not artifacts.is_dir() or not any(path.is_file() for path in artifacts.rglob("*"))):
        raise RequestError("PDF extraction requires pre-provisioned local Docling artifacts")
    docling_version()
    return token, root, pdf_enabled, artifacts


def selected_folder(raw: str) -> tuple[Path, str]:
    if not isinstance(raw, str):
        raise RequestError("an explicit selected document folder is required")
    value = raw.strip().replace("\\", "/")
    candidate = PurePosixPath(value)
    if not value or value == "." or candidate.is_absolute() or ".." in candidate.parts or len(value) > 400:
        raise RequestError("document folder must stay inside the selected intake root")
    _, root, _, _ = configured()
    folder = (root / Path(*candidate.parts)).resolve()
    if root not in folder.parents or not folder.is_dir() or folder.is_symlink():
        raise RequestError("selected document folder is unavailable")
    return folder, candidate.as_posix()


def extension_format(path: Path, pdf_enabled: bool) -> str | None:
    suffix = path.suffix.lower()
    if suffix == ".pdf":
        return "pdf" if pdf_enabled else None
    return TEXT_FORMATS.get(suffix) or DOCLING_FORMATS.get(suffix)


def candidate_files(folder: Path, pdf_enabled: bool) -> list[tuple[Path, str]]:
    files: list[tuple[Path, str]] = []
    entries_seen = 0
    for entry in folder.rglob("*"):
        entries_seen += 1
        if entries_seen > MAX_FILESYSTEM_ENTRIES:
            break
        if len(files) >= MAX_DOCUMENTS:
            break
        if entry.is_symlink() or not entry.is_file():
            continue
        file_format = extension_format(entry, pdf_enabled)
        if file_format is None:
            continue
        try:
            if entry.stat().st_size <= 0 or entry.stat().st_size > MAX_DOCUMENT_BYTES:
                continue
        except OSError:
            continue
        files.append((entry, file_format))
    return sorted(files, key=lambda item: item[0].as_posix())


def converter(pdf_enabled: bool, artifacts: Path):
    # Keep the imports lazy so health checks and unit tests cannot trigger model
    # initialization, downloads, or a large import path.
    from docling.datamodel.base_models import InputFormat
    from docling.document_converter import DocumentConverter, PdfFormatOption

    allowed_formats = [InputFormat.DOCX, InputFormat.PPTX, InputFormat.XLSX, InputFormat.HTML]
    format_options = {}
    if pdf_enabled:
        from docling.datamodel.pipeline_options import PdfPipelineOptions

        pipeline_options = PdfPipelineOptions(
            do_ocr=False,
            do_table_structure=False,
            do_code_enrichment=False,
            do_formula_enrichment=False,
            do_picture_classification=False,
            do_picture_description=False,
            do_chart_extraction=False,
            force_backend_text=True,
            enable_remote_services=False,
            allow_external_plugins=False,
            artifacts_path=str(artifacts),
            document_timeout=120,
        )
        allowed_formats.append(InputFormat.PDF)
        format_options[InputFormat.PDF] = PdfFormatOption(pipeline_options=pipeline_options)
    return DocumentConverter(allowed_formats=allowed_formats, format_options=format_options)


def extract_text(path: Path, file_format: str, pdf_enabled: bool, artifacts: Path, document_converter) -> tuple[str, int]:
    if file_format in TEXT_FORMATS.values():
        text = path.read_text(encoding="utf-8", errors="replace").strip()
        return text, 0
    result = document_converter.convert(path, max_num_pages=MAX_PAGES, max_file_size=MAX_DOCUMENT_BYTES)
    text = result.document.export_to_markdown().strip()
    pages = getattr(result.document, "pages", {})
    return text, len(pages) if hasattr(pages, "__len__") else 0


def require_token(headers) -> None:
    token, _, _, _ = configured()
    if not compare_digest(token, headers.get("X-HAI-Docling-Token", "")):
        raise PermissionError("invalid runner token")


def extract(folder_name: str) -> dict:
    folder, relative = selected_folder(folder_name)
    _, _, pdf_enabled, artifacts = configured()
    candidates = candidate_files(folder, pdf_enabled)
    # Text and Markdown need no Docling import or converter initialization.
    # This keeps routine low-cost source intake within the runner's small path.
    requires_converter = any(file_format not in TEXT_FORMATS.values() for _, file_format in candidates)
    document_converter = converter(pdf_enabled, artifacts) if requires_converter else None
    documents = []
    total_chars = 0
    for document_path, file_format in candidates:
        text, pages = extract_text(document_path, file_format, pdf_enabled, artifacts, document_converter)
        text = text.strip()
        if not text or len(text) > MAX_DOCUMENT_CHARS or pages < 0 or pages > MAX_PAGES:
            raise RequestError("document extraction exceeded its fixed output boundary")
        total_chars += len(text)
        if total_chars > MAX_TOTAL_CHARS:
            raise RequestError("document extraction exceeded its fixed total output boundary")
        relative_path = f"{relative}/{document_path.relative_to(folder).as_posix()}"
        documents.append({
            "path": relative_path,
            "text": text,
            "format": file_format,
            "pageCount": pages,
            "contentDigest": sha256(text.encode("utf-8")).hexdigest(),
        })
    return {"status": "completed", "engine": f"docling {docling_version()}", "documents": documents}


def send_json(handler: BaseHTTPRequestHandler, status: HTTPStatus, body: dict) -> None:
    data = json.dumps(body, separators=(",", ":")).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Cache-Control", "no-store")
    handler.send_header("Content-Length", str(len(data)))
    handler.end_headers()
    handler.wfile.write(data)


class Handler(BaseHTTPRequestHandler):
    server_version = "HAI-Docling/1.0"

    def log_message(self, _format: str, *_args: object) -> None:
        return

    def do_GET(self) -> None:
        if self.path != "/healthz":
            send_json(self, HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        try:
            configured()
            send_json(self, HTTPStatus.OK, {"status": "ok", "engine": f"docling {docling_version()}", "configured": True})
        except RequestError:
            send_json(self, HTTPStatus.OK, {"status": "ok", "engine": "docling unavailable", "configured": False})

    def do_POST(self) -> None:
        if self.path != "/v1/extract":
            send_json(self, HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            send_json(self, HTTPStatus.BAD_REQUEST, {"error": "invalid content length"})
            return
        if length <= 0 or length > MAX_REQUEST_BYTES:
            send_json(self, HTTPStatus.REQUEST_ENTITY_TOO_LARGE, {"error": "request must contain one bounded folder"})
            return
        try:
            require_token(self.headers)
            body = json.loads(self.rfile.read(length))
            if set(body) != {"folder"}:
                raise RequestError("runner accepts only one configured selected folder")
            folder = body["folder"]
        except PermissionError:
            send_json(self, HTTPStatus.UNAUTHORIZED, {"error": "unauthorized"})
            return
        except (RequestError, ValueError, TypeError):
            send_json(self, HTTPStatus.BAD_REQUEST, {"error": "local document extraction request is invalid"})
            return
        if not RUN_LOCK.acquire(blocking=False):
            send_json(self, HTTPStatus.CONFLICT, {"error": "a local document extraction is already running"})
            return
        try:
            send_json(self, HTTPStatus.OK, extract(folder))
        except (RequestError, OSError, ValueError, RuntimeError):
            send_json(self, HTTPStatus.BAD_GATEWAY, {"error": "local document extraction could not complete"})
        finally:
            RUN_LOCK.release()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
