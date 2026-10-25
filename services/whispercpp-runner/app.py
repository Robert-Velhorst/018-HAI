"""Internal-only whisper.cpp runner for HAI source intake.

This service accepts one relative folder inside /intake, finds bounded local
audio files, and returns text only. It does not expose a microphone, accept
media uploads, download models, call the network, or retain raw audio/output.
"""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import tempfile
import threading
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path, PurePosixPath

ENGINE = "whisper.cpp v1.9.5"
INTAKE_ROOT = Path("/intake")
MODEL_ROOT = Path("/models")
MAX_REQUEST_BYTES = 1024
MAX_TRANSCRIPT_CHARS = 100_000
AUDIO_EXTENSIONS = {".wav", ".mp3", ".m4a", ".ogg", ".flac", ".aac", ".opus"}
TOKEN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$")
RUN_LOCK = threading.Lock()


def env_int(name: str, default: int, minimum: int, maximum: int) -> int:
    try:
        value = int(os.environ.get(name, default))
    except ValueError:
        value = default
    return min(max(value, minimum), maximum)


def config() -> tuple[Path, str, int, int, int]:
    model_id = os.environ.get("WHISPER_CPP_MODEL_FILE", "").strip()
    if not TOKEN.fullmatch(model_id):
        raise ValueError("a bounded WHISPER_CPP_MODEL_FILE is required")
    model = (MODEL_ROOT / model_id).resolve()
    if MODEL_ROOT not in model.parents or not model.is_file():
        raise ValueError("configured whisper.cpp model file is unavailable")
    language = os.environ.get("WHISPER_CPP_LANGUAGE", "auto").strip()
    if not TOKEN.fullmatch(language):
        raise ValueError("WHISPER_CPP_LANGUAGE is invalid")
    return (
        model,
        language,
        env_int("WHISPER_CPP_FILE_LIMIT", 25, 1, 25),
        env_int("WHISPER_CPP_MAX_AUDIO_BYTES", 67_108_864, 1_024, 67_108_864),
        env_int("WHISPER_CPP_TIMEOUT_SECONDS", 180, 10, 600),
    )


def selected_folder(raw: str) -> tuple[Path, str]:
    if not isinstance(raw, str):
        raise ValueError("an explicit selected audio folder is required")
    value = raw.strip().replace("\\", "/")
    candidate = PurePosixPath(value)
    if not value or value == "." or candidate.is_absolute() or ".." in candidate.parts or len(value) > 400:
        raise ValueError("audio folder must stay inside the selected intake root")
    folder = (INTAKE_ROOT / Path(*candidate.parts)).resolve()
    if INTAKE_ROOT not in folder.parents or not folder.is_dir():
        raise ValueError("selected audio folder is unavailable")
    return folder, candidate.as_posix()


def audio_files(folder: Path, limit: int, maximum_bytes: int) -> list[Path]:
    files: list[Path] = []
    for entry in sorted(folder.rglob("*")):
        if len(files) >= limit:
            break
        if entry.is_symlink() or not entry.is_file() or entry.suffix.lower() not in AUDIO_EXTENSIONS:
            continue
        try:
            if entry.stat().st_size > maximum_bytes:
                continue
        except OSError:
            continue
        files.append(entry)
    return files


def transcribe_file(audio: Path, model: Path, language: str, timeout: int) -> str:
    work = Path(tempfile.mkdtemp(prefix="hai-whisper-"))
    try:
        wav = work / "input.wav"
        output = work / "transcript"
        subprocess.run(
            ["ffmpeg", "-nostdin", "-v", "error", "-y", "-i", str(audio), "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", str(wav)],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=timeout,
        )
        subprocess.run(
            ["whisper-cli", "-m", str(model), "-f", str(wav), "-l", language, "-otxt", "-of", str(output), "-nt"],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=timeout,
        )
        text = (output.with_suffix(".txt")).read_text(encoding="utf-8", errors="replace").strip()
        if not text or len(text) > MAX_TRANSCRIPT_CHARS:
            raise ValueError("transcription output is empty or exceeded its bounded size")
        return text
    finally:
        shutil.rmtree(work, ignore_errors=True)


class Handler(BaseHTTPRequestHandler):
    server_version = "HAIWhisperCPP/1.0"

    def log_message(self, _format: str, *_args: object) -> None:
        return

    def send_json(self, status: HTTPStatus, body: dict) -> None:
        data = json.dumps(body, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self) -> None:
        if self.path != "/healthz":
            self.send_json(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        try:
            model, _language, _limit, _bytes, _timeout = config()
            self.send_json(HTTPStatus.OK, {"status": "ok", "engine": ENGINE, "configured": True, "modelId": model.name})
        except ValueError:
            self.send_json(HTTPStatus.OK, {"status": "ok", "engine": ENGINE, "configured": False})

    def do_POST(self) -> None:
        if self.path != "/v1/transcribe":
            self.send_json(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        if not RUN_LOCK.acquire(blocking=False):
            self.send_json(HTTPStatus.CONFLICT, {"error": "a local transcription run is already in progress"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > MAX_REQUEST_BYTES:
                raise ValueError("transcription requires one bounded folder request")
            body = json.loads(self.rfile.read(length))
            if set(body) != {"folder"}:
                raise ValueError("runner accepts only one configured selected folder")
            model, language, limit, maximum_bytes, timeout = config()
            folder, relative = selected_folder(body["folder"])
            transcripts = []
            for audio in audio_files(folder, limit, maximum_bytes):
                text = transcribe_file(audio, model, language, timeout)
                transcripts.append({"path": f"{relative}/{audio.relative_to(folder).as_posix()}", "text": text, "modelId": model.name, "language": language})
            self.send_json(HTTPStatus.OK, {"status": "completed", "engine": ENGINE, "transcripts": transcripts})
        except (ValueError, json.JSONDecodeError, OSError, subprocess.SubprocessError):
            self.send_json(HTTPStatus.BAD_REQUEST, {"error": "local transcription could not complete"})
        finally:
            RUN_LOCK.release()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
