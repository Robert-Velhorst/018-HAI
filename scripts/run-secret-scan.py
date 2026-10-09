"""Run a pinned Gitleaks scan without exposing candidate-bearing output."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path
from typing import Callable, Sequence


ROOT = Path(__file__).resolve().parents[1]
GITLEAKS_MODULE = "github.com/gitleaks/gitleaks/v8@v8.30.1"


def scan(
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    root: Path = ROOT,
) -> int:
    command: Sequence[str] = (
        "go",
        "run",
        GITLEAKS_MODULE,
        "git",
        "--source",
        str(root),
        "--redact=100",
        "--no-banner",
        "--log-level=fatal",
        "--report-format=json",
        "--report-path=-",
    )
    try:
        result = runner(
            list(command),
            cwd=root,
            text=True,
            capture_output=True,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        print("Secret scan failed; scanner output was withheld.")
        return 2

    if result.returncode != 0:
        print("Secret scan failed; scanner output was withheld to protect candidate data.")
        return 1

    print("Secret scan passed.")
    return 0


if __name__ == "__main__":
    sys.exit(scan())
