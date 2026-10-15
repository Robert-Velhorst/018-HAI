#!/usr/bin/env python3
"""Verify Docker's effective build context honors each service's ignore rules."""

from __future__ import annotations

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
DOCKERFILE = "FROM scratch\nCOPY . /context/\n"

SERVICE_FIXTURES = {
    "backend": {
        "included": (
            "go.mod",
            "cmd/fixture.go",
            "migrations/001_fixture.sql",
            "docs/fixture.md",
        ),
        "excluded": ("internal/fixture_test.go", "testdata/fixture.json"),
    },
    "frontend": {
        "included": (
            "package.json",
            "src/main.ts",
            "src/assets/public-ca.crt",
            "docs/fixture.md",
        ),
        "excluded": ("dist/fixture.js", "coverage/fixture.out"),
    },
    "idp": {
        "included": (
            "go.mod",
            "cmd/main.go",
            "migrations/001_fixture.sql",
            "docs/fixture.md",
        ),
        "excluded": ("internal/fixture_test.go", "testdata/fixture.json"),
    },
}

SENSITIVE_FIXTURES = (
    ".env",
    "nested/.env.local",
    "nested/credentials.json",
    "nested/private.key",
    "nested/private.pem",
    "node_modules/fixture/index.js",
)


class DockerBuildContextTest(unittest.TestCase):
    def test_service_ignore_files_exclude_sensitive_and_local_files(self) -> None:
        docker = subprocess.run(
            ["docker", "buildx", "version"],
            check=False,
            capture_output=True,
            text=True,
            timeout=20,
        )
        self.assertEqual(
            docker.returncode,
            0,
            "Docker Buildx must be available to validate effective build contexts: "
            + docker.stderr.strip(),
        )

        for service, expectations in SERVICE_FIXTURES.items():
            with self.subTest(service=service), tempfile.TemporaryDirectory(
                prefix=f"hai-{service}-context-"
            ) as temp:
                temporary_root = Path(temp)
                context = temporary_root / "context"
                context.mkdir()
                (context / ".dockerignore").write_bytes(
                    (ROOT / service / ".dockerignore").read_bytes()
                )
                for relative in (
                    *expectations["included"],
                    *expectations["excluded"],
                    *SENSITIVE_FIXTURES,
                ):
                    fixture = context / relative
                    fixture.parent.mkdir(parents=True, exist_ok=True)
                    fixture.write_text(f"fixture:{relative}\n", encoding="utf-8")

                output = temporary_root / "export"
                environment = os.environ.copy()
                environment["DOCKER_BUILDKIT"] = "1"
                result = subprocess.run(
                    [
                        "docker",
                        "buildx",
                        "build",
                        "--no-cache",
                        "--progress=quiet",
                        "--file",
                        "-",
                        "--output",
                        f"type=local,dest={output}",
                        str(context),
                    ],
                    input=DOCKERFILE,
                    check=False,
                    capture_output=True,
                    text=True,
                    timeout=180,
                    env=environment,
                )
                self.assertEqual(
                    result.returncode,
                    0,
                    "BuildKit failed to export the synthetic context: "
                    + result.stderr[-4000:],
                )

                exported = output / "context"
                for relative in expectations["included"]:
                    with self.subTest(service=service, included=relative):
                        self.assertTrue((exported / relative).is_file())
                for relative in (*expectations["excluded"], *SENSITIVE_FIXTURES):
                    with self.subTest(service=service, excluded=relative):
                        self.assertFalse((exported / relative).exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
