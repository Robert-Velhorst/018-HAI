"""Contract tests for the isolated Python runner container smoke stage."""

from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
import python_runner_container_smoke as smoke  # noqa: E402


WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")


def completed(command, returncode=0, stdout="", stderr=""):
    return subprocess.CompletedProcess(command, returncode, stdout, stderr)


class PythonRunnerContainerSmokeTest(unittest.TestCase):
    def test_smoke_policy_covers_exactly_the_python_runner_matrix(self):
        matrix = set(
            __import__("re").findall(
                r"(?m)^          - name: ([a-z0-9-]+)$", WORKFLOW
            )
        )
        self.assertEqual(matrix, set(smoke.RUNNER_POLICIES))
        self.assertEqual(len(smoke.RUNNER_POLICIES), 20)
        for runner, policy in smoke.RUNNER_POLICIES.items():
            with self.subTest(runner=runner):
                self.assertGreater(policy.memory_mb, 0)
                self.assertLessEqual(policy.memory_mb, 1536)
                self.assertGreater(float(policy.cpu_count), 0)
                self.assertLessEqual(float(policy.cpu_count), 0.75)
                self.assertGreater(policy.startup_seconds, 0)
                self.assertLessEqual(policy.startup_seconds, 75)

    def test_workflow_runs_smoke_after_build_with_a_step_deadline(self):
        job_start = WORKFLOW.index("  python-runners:")
        job_end = WORKFLOW.index("\n  secret-scan:", job_start)
        job = WORKFLOW[job_start:job_end]
        build = job.index("- name: Build runner container image")
        smoke_step = job.index("- name: Start and health-check isolated runner container")
        self.assertLess(build, smoke_step)
        step = job[smoke_step:]
        self.assertIn("timeout-minutes: 2", step)
        self.assertIn("scripts/python_runner_container_smoke.py", step)
        self.assertIn('--image "hai-${{ matrix.name }}:ci"', step)
        self.assertIn("test_python_runner_container_smoke.py", WORKFLOW)

    def test_container_command_has_no_network_mount_or_credentials(self):
        command = smoke.docker_run_command(
            "docling-runner", "hai-docling-runner:ci", "smoke-test", Path("cid")
        )
        self.assertIn("none", command)
        self.assertIn("--pull=never", command)
        self.assertIn("--read-only", command)
        self.assertIn("--cap-drop", command)
        self.assertIn("ALL", command)
        self.assertIn("--pids-limit", command)
        self.assertIn("--memory-swap", command)
        self.assertIn("768m", command)
        self.assertIn("0.50", command)
        self.assertIn("/tmp:rw,nosuid,nodev,size=64m", command)
        self.assertFalse(any(arg in {"-p", "--publish", "--network=host"} for arg in command))
        self.assertFalse(any(arg.startswith("--volume") or arg == "-v" for arg in command))
        self.assertFalse(any("TOKEN=" in arg or "API_KEY=" in arg for arg in command))

    def test_runner_readiness_contracts_use_health_route_or_mcp_auth_boundary(self):
        for runner in set(smoke.RUNNER_POLICIES) - {"fastmcp-bridge"}:
            with self.subTest(runner=runner):
                command = smoke.health_probe_command(runner, "container-id")
                self.assertIn("/healthz", command[-1])
                self.assertIn("b.get('status') == 'ok'", command[-1])
        mcp_probe = smoke.health_probe_command("fastmcp-bridge", "container-id")[-1]
        self.assertIn("/mcp", mcp_probe)
        self.assertIn("code in (200, 401, 405, 406)", mcp_probe)
        self.assertNotIn("Authorization", mcp_probe)
        self.assertEqual(
            smoke.health_probe_command("gosec-runner", "container-id")[3], "python3"
        )
        self.assertEqual(
            smoke.health_probe_command("whispercpp-runner", "container-id")[3], "python3"
        )

    def test_model_runner_environment_is_offline_and_fastmcp_uses_only_synthetic_config(self):
        environment = smoke.container_environment("docling-runner")
        self.assertEqual(environment["HF_HUB_OFFLINE"], "1")
        self.assertEqual(environment["TRANSFORMERS_OFFLINE"], "1")
        self.assertEqual(environment["DATASETS_OFFLINE"], "1")
        bridge = smoke.container_environment("fastmcp-bridge")
        self.assertEqual(bridge["HAI_FASTMCP_BRIDGE_ENABLED"], "true")
        self.assertEqual(len(bridge["HAI_FASTMCP_BRIDGE_TOKEN"]), 51)
        self.assertEqual(len(bridge["HAI_FASTMCP_CLIENT_TOKEN"]), 52)
        self.assertEqual(
            bridge["HAI_FASTMCP_BRIDGE_API_BASE_URL"],
            "http://127.0.0.1:1/api/v1/mcp-agent",
        )

    def test_grype_liveness_smoke_uses_synthetic_configuration_and_empty_mounts(self):
        environment = smoke.container_environment("grype-runner")
        self.assertEqual(environment["HAI_GRYPE_WORKSPACES"], "ci-smoke")
        self.assertGreaterEqual(len(environment["HAI_GRYPE_RUNNER_TOKEN"]), 16)
        command = smoke.docker_run_command(
            "grype-runner", "hai-grype-runner:ci", "smoke-test", Path("cid")
        )
        self.assertIn("/inputs:ro,nosuid,nodev,size=1m", command)
        self.assertIn("/grype-db:ro,nosuid,nodev,size=1m", command)

    def test_successful_smoke_removes_the_started_container(self):
        commands = []

        def fake_run(command, **_kwargs):
            commands.append(command)
            if command[1] == "run":
                Path(command[command.index("--cidfile") + 1]).write_text(
                    "container-id\n", encoding="utf-8"
                )
            if command[1] == "inspect":
                return completed(command, stdout="true\n")
            return completed(command)

        smoke.run_smoke(
            "gitleaks-runner",
            "hai-gitleaks-runner:ci",
            run_process=fake_run,
            monotonic=lambda: 0,
            sleep=lambda _seconds: None,
        )
        self.assertEqual(commands[0][1], "run")
        self.assertTrue(
            any(command[1:4] == ["rm", "--force", "container-id"] for command in commands)
        )

    def test_failed_readiness_still_removes_the_container(self):
        commands = []

        def fake_run(command, **_kwargs):
            commands.append(command)
            if command[1] == "run":
                Path(command[command.index("--cidfile") + 1]).write_text(
                    "container-id\n", encoding="utf-8"
                )
            if command[1] == "inspect":
                return completed(command, stdout="true\n")
            if command[1] == "exec":
                return completed(command, returncode=1, stderr="not ready")
            return completed(command)

        ticks = iter(range(1000))
        with self.assertRaisesRegex(RuntimeError, "container smoke failed"):
            smoke.run_smoke(
                "ortools-solver",
                "hai-ortools-solver:ci",
                run_process=fake_run,
                monotonic=lambda: float(next(ticks)),
                sleep=lambda _seconds: None,
            )
        self.assertTrue(
            any(command[1:4] == ["rm", "--force", "container-id"] for command in commands)
        )

    def test_unlisted_or_mismatched_image_fails_before_docker(self):
        with patch.object(smoke, "_run") as run:
            with self.assertRaisesRegex(ValueError, "no bounded smoke policy"):
                smoke.run_smoke("unlisted-runner", "unlisted:ci")
            with self.assertRaisesRegex(ValueError, "unexpected image reference"):
                smoke.run_smoke("gitleaks-runner", "other-image:latest")
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main(verbosity=2)
