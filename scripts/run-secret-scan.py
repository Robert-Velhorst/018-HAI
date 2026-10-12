"""Run a pinned Gitleaks scan without exposing candidate-bearing output."""

from __future__ import annotations

import subprocess
import sys
import json
import os
import re
from pathlib import Path
from typing import Callable, Sequence


ROOT = Path(__file__).resolve().parents[1]
GITLEAKS_MODULE = "github.com/zricethezav/gitleaks/v8@v8.30.1"


def _safe_finding_summary(stdout: str) -> list[str] | None:
    if not isinstance(stdout, str):
        return None
    candidates = [stdout.strip()]
    first_array = stdout.find("[")
    last_array = stdout.rfind("]")
    if first_array >= 0 and last_array > first_array:
        candidates.append(stdout[first_array : last_array + 1])
    findings = None
    for candidate in candidates:
        try:
            findings = json.loads(candidate)
            break
        except json.JSONDecodeError:
            continue
    if not isinstance(findings, list):
        return None
    lines = [f"Secret scan found {len(findings)} candidate(s); values are redacted."]
    finding_limit = 100
    for finding in findings[:finding_limit]:
        if not isinstance(finding, dict):
            continue
        rule = finding.get("RuleID")
        path = finding.get("File")
        line = finding.get("StartLine")
        commit = finding.get("Commit")
        safe_rule = rule[:64] if isinstance(rule, str) and re.fullmatch(r"[A-Za-z0-9_.-]+", rule) else "unknown-rule"
        safe_path = path.replace("\\", "/")[:180] if isinstance(path, str) else "unknown-path"
        safe_path = "".join(ch for ch in safe_path if ch.isprintable())
        safe_line = str(line) if isinstance(line, int) and line > 0 else "?"
        safe_commit = commit[:12] if isinstance(commit, str) and re.fullmatch(r"[0-9a-fA-F]{7,64}", commit) else "unknown"
        lines.append(f"- {safe_rule}: {safe_path}:{safe_line} (commit {safe_commit})")
    if len(findings) > finding_limit:
        lines.append(f"- {len(findings) - finding_limit} additional finding(s) omitted")
    return lines


def scan(
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    root: Path = ROOT,
    base_revision: str | None = None,
) -> int:
    if base_revision is None:
        base_revision = os.environ.get("GITLEAKS_BASE_SHA", "").strip()
    if base_revision and not re.fullmatch(r"[0-9a-fA-F]{40}", base_revision):
        print("Secret scan failed; comparison revision is invalid.")
        return 2
    if os.environ.get("CI", "").lower() == "true" and not base_revision:
        print("Secret scan failed; CI did not provide a comparison revision.")
        return 2

    command: list[str] = [
        "go",
        "run",
        GITLEAKS_MODULE,
        "git",
        str(root),
        "--redact=100",
        "--no-banner",
        "--log-level=fatal",
        "--report-format=json",
        "--report-path=-",
    ]
    if base_revision:
        command.extend(("--log-opts", f"{base_revision}..HEAD"))
    try:
        result = runner(
            command,
            cwd=root,
            text=True,
            capture_output=True,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        print("Secret scan failed; scanner output was withheld.")
        return 2

    if result.returncode != 0:
        summary = _safe_finding_summary(result.stdout)
        if summary is None:
            print("Secret scan failed; scanner output was withheld because its report was not valid JSON.")
        else:
            print("\n".join(summary))
        return 1

    print("Secret scan passed.")
    return 0


if __name__ == "__main__":
    sys.exit(scan())
