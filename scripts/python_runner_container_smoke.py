"""Start one built Python runner image under strict, offline smoke limits."""

from __future__ import annotations

import argparse
from dataclasses import dataclass
import subprocess
import tempfile
import time
from pathlib import Path
from typing import Callable, Sequence
import uuid


@dataclass(frozen=True)
class RunnerPolicy:
    memory_mb: int
    cpu_count: str
    startup_seconds: int
    health_path: str = "/healthz"


RUNNER_POLICIES = {
    "agent-framework-runner": RunnerPolicy(768, "0.50", 45),
    "crewai-runner": RunnerPolicy(1536, "0.75", 75),
    "deepeval-runner": RunnerPolicy(1024, "0.50", 60),
    "deepteam-runner": RunnerPolicy(1024, "0.50", 60),
    "docling-runner": RunnerPolicy(768, "0.50", 60),
    "evidently-runner": RunnerPolicy(768, "0.50", 60),
    "fastmcp-bridge": RunnerPolicy(512, "0.50", 45, "/mcp"),
    "garak-runner": RunnerPolicy(1024, "0.50", 60),
    "gitleaks-runner": RunnerPolicy(512, "0.50", 30),
    "gosec-runner": RunnerPolicy(512, "0.50", 30),
    "grype-runner": RunnerPolicy(512, "0.50", 30),
    "guardrails-runner": RunnerPolicy(1024, "0.50", 60),
    "lm-eval-runner": RunnerPolicy(1536, "0.50", 75),
    "miniswe-runner": RunnerPolicy(768, "0.50", 45),
    "ortools-solver": RunnerPolicy(512, "0.50", 30),
    "pydantic-ai-runner": RunnerPolicy(768, "0.50", 45),
    "syft-runner": RunnerPolicy(512, "0.50", 30),
    "trivy-runner": RunnerPolicy(512, "0.50", 30),
    "wasi-runner": RunnerPolicy(512, "0.50", 30),
    "whispercpp-runner": RunnerPolicy(512, "0.50", 45),
}

PYTHON3_EXECUTABLE_RUNNERS = {"gosec-runner", "whispercpp-runner"}

BASE_ENVIRONMENT = {
    "HOME": "/tmp",
    "DO_NOT_TRACK": "true",
    "OTEL_SDK_DISABLED": "true",
    "HF_HUB_DISABLE_TELEMETRY": "1",
    "HF_HUB_OFFLINE": "1",
    "TRANSFORMERS_OFFLINE": "1",
    "DATASETS_OFFLINE": "1",
    "HF_HOME": "/tmp/huggingface",
    "XDG_CACHE_HOME": "/tmp/cache",
    "PROMPTFOO_DISABLE_TELEMETRY": "true",
    "PROMPTFOO_DISABLE_UPDATE": "1",
    "ANONYMIZED_TELEMETRY": "False",
    "PYTHONDONTWRITEBYTECODE": "1",
    "PYTHONUNBUFFERED": "1",
}

PROCESS_RUN = Callable[..., subprocess.CompletedProcess[str]]


def container_environment(runner: str) -> dict[str, str]:
    environment = dict(BASE_ENVIRONMENT)
    if runner == "fastmcp-bridge":
        environment.update(
            {
                "HAI_FASTMCP_BRIDGE_ENABLED": "true",
                "HAI_FASTMCP_BRIDGE_TOKEN": "smoke-bridge-token-" + "x" * 32,
                "HAI_FASTMCP_CLIENT_TOKEN": "smoke-client-token--" + "y" * 32,
                "HAI_FASTMCP_BRIDGE_API_BASE_URL": "http://127.0.0.1:1/api/v1/mcp-agent",
            }
        )
    elif runner == "grype-runner":
        environment.update(
            {
                "HAI_GRYPE_RUNNER_TOKEN": "synthetic-grype-smoke-token-123456",
                "HAI_GRYPE_WORKSPACES": "ci-smoke",
            }
        )
    return environment


def docker_run_command(
    runner: str, image: str, name: str, cidfile: Path, docker: str = "docker"
) -> list[str]:
    policy = RUNNER_POLICIES[runner]
    command = [
        docker,
        "run",
        "--detach",
        "--rm",
        "--pull=never",
        "--cidfile",
        str(cidfile),
        "--name",
        name,
        "--network",
        "none",
        "--cpus",
        policy.cpu_count,
        "--memory",
        f"{policy.memory_mb}m",
        "--memory-swap",
        f"{policy.memory_mb}m",
        "--pids-limit",
        "128",
        "--read-only",
        "--tmpfs",
        "/tmp:rw,nosuid,nodev,size=64m",
        "--cap-drop",
        "ALL",
        "--security-opt",
        "no-new-privileges:true",
    ]
    for key, value in container_environment(runner).items():
        command.extend(("--env", f"{key}={value}"))
    if runner == "grype-runner":
        command.extend(("--tmpfs", "/inputs:ro,nosuid,nodev,size=1m"))
        command.extend(("--tmpfs", "/grype-db:ro,nosuid,nodev,size=1m"))
    command.extend((image,))
    return command


def health_probe_command(runner: str, container_id: str, docker: str = "docker") -> list[str]:
    path = RUNNER_POLICIES[runner].health_path
    if runner == "fastmcp-bridge":
        probe = (
            "from urllib.error import HTTPError\n"
            "from urllib.request import urlopen\n"
            "try:\n"
            "    response = urlopen('http://127.0.0.1:8080/mcp', timeout=2)\n"
            "    code = response.status\n"
            "    response.close()\n"
            "except HTTPError as error:\n"
            "    code = error.code\n"
            "assert code in (200, 401, 405, 406), f'unexpected MCP readiness status: {code}'"
        )
        executable, argument = "python", "-c"
    else:
        probe = (
            "import json; from urllib.request import urlopen; "
            f"r=urlopen('http://127.0.0.1:8080{path}', timeout=2); "
            "b=json.loads(r.read(8192)); "
            "assert r.status == 200 and b.get('status') == 'ok', 'health contract failed'"
        )
        executable = "python3" if runner in PYTHON3_EXECUTABLE_RUNNERS else "python"
        argument = "-c"
    return [docker, "exec", container_id, executable, argument, probe]


def _run(
    command: Sequence[str],
    *,
    timeout: float,
    run_process: PROCESS_RUN,
) -> subprocess.CompletedProcess[str]:
    return run_process(
        list(command), capture_output=True, text=True, timeout=timeout, check=False
    )


def run_smoke(
    runner: str,
    image: str,
    *,
    docker: str = "docker",
    run_process: PROCESS_RUN = subprocess.run,
    monotonic: Callable[[], float] = time.monotonic,
    sleep: Callable[[float], None] = time.sleep,
) -> None:
    if runner not in RUNNER_POLICIES:
        raise ValueError(f"runner has no bounded smoke policy: {runner}")
    if image != f"hai-{runner}:ci":
        raise ValueError(f"unexpected image reference for {runner}: {image}")

    policy = RUNNER_POLICIES[runner]
    name = f"hai-smoke-{runner}-{uuid.uuid4().hex[:12]}"
    container_id = name
    with tempfile.TemporaryDirectory(prefix="hai-runner-smoke-") as temporary:
        cidfile = Path(temporary) / "container-id"
        try:
            deadline = monotonic() + policy.startup_seconds
            started = _run(
                docker_run_command(runner, image, name, cidfile, docker),
                timeout=min(10, policy.startup_seconds),
                run_process=run_process,
            )
            if started.returncode != 0:
                raise RuntimeError(
                    f"docker run failed for {runner}: "
                    f"{(started.stderr or started.stdout)[-2000:]}"
                )
            if cidfile.is_file():
                container_id = cidfile.read_text(encoding="utf-8").strip() or name

            last_error = "container did not become ready before the startup deadline"
            while monotonic() < deadline:
                remaining = deadline - monotonic()
                if remaining <= 0:
                    break
                state = _run(
                    [docker, "inspect", "--format={{.State.Running}}", container_id],
                    timeout=min(5, remaining),
                    run_process=run_process,
                )
                if state.returncode != 0 or state.stdout.strip().lower() != "true":
                    last_error = "container exited before readiness"
                    break
                remaining = deadline - monotonic()
                if remaining <= 0:
                    break
                probe = _run(
                    health_probe_command(runner, container_id, docker),
                    timeout=min(5, remaining),
                    run_process=run_process,
                )
                if probe.returncode == 0:
                    print(
                        f"{runner}: ready via {policy.health_path}; "
                        f"cpu={policy.cpu_count}, memory={policy.memory_mb}MiB, "
                        f"startup_deadline={policy.startup_seconds}s"
                    )
                    return
                last_error = (probe.stderr or probe.stdout or "readiness probe failed")[-1000:]
                sleep(min(1, max(0, deadline - monotonic())))
            raise RuntimeError(f"{runner} container smoke failed: {last_error}")
        finally:
            cleanup = _run(
                [docker, "rm", "--force", container_id],
                timeout=10,
                run_process=run_process,
            )
            if cleanup.returncode != 0:
                detail = (cleanup.stderr or cleanup.stdout).lower()
                if "no such container" not in detail and "not found" not in detail:
                    raise RuntimeError(
                        f"container cleanup failed for {runner}: "
                        f"{(cleanup.stderr or cleanup.stdout)[-1000:]}"
                    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runner", required=True, choices=sorted(RUNNER_POLICIES))
    parser.add_argument("--image", required=True)
    parser.add_argument("--docker", default="docker")
    arguments = parser.parse_args()
    try:
        run_smoke(arguments.runner, arguments.image, docker=arguments.docker)
    except (OSError, RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
        print(f"Python runner container smoke failed: {error}")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
