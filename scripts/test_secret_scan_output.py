"""Ensure secret-scanner output never echoes a candidate value."""

from __future__ import annotations

from contextlib import redirect_stdout
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import Mock, patch


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "run_secret_scan", ROOT / "scripts" / "run-secret-scan.py"
)
assert SPEC and SPEC.loader
secret_scan = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(secret_scan)


@patch.dict("os.environ", {"CI": "", "GITLEAKS_BASE_SHA": ""})
class SecretScanOutputTest(unittest.TestCase):
    def test_candidate_output_is_suppressed_on_findings(self) -> None:
        candidate = "ghp_" + "A" * 36
        runner = Mock(
            return_value=subprocess.CompletedProcess(
                args=[], returncode=1, stdout=candidate, stderr=f"finding: {candidate}"
            )
        )
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 1)
        self.assertIn("withheld", output.getvalue())
        self.assertNotIn(candidate, output.getvalue())
        self.assertNotIn(candidate, runner.call_args.args[0])
        self.assertTrue(runner.call_args.kwargs["capture_output"])
        self.assertIn("--redact=100", runner.call_args.args[0])
        self.assertIn("--report-path=-", runner.call_args.args[0])

    def test_scanner_start_failure_is_generic(self) -> None:
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(
                runner=Mock(side_effect=OSError("synthetic candidate detail")), root=ROOT
            )

        self.assertEqual(result, 2)
        self.assertNotIn("synthetic candidate detail", output.getvalue())

    def test_ci_scan_requires_valid_comparison_revision(self) -> None:
        runner = Mock()
        output = io.StringIO()
        with patch.dict("os.environ", {"CI": "true", "GITLEAKS_BASE_SHA": ""}):
            with redirect_stdout(output):
                result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 2)
        self.assertIn("did not provide a comparison revision", output.getvalue())
        runner.assert_not_called()

    def test_scan_limits_history_to_the_provided_base_revision(self) -> None:
        base_revision = "a" * 40
        runner = Mock(
            return_value=subprocess.CompletedProcess(args=[], returncode=0, stdout="", stderr="")
        )
        with patch.dict("os.environ", {"CI": "true", "GITLEAKS_BASE_SHA": base_revision}):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 0)
        command = runner.call_args.args[0]
        self.assertIn("--log-opts", command)
        self.assertEqual(command[command.index("--log-opts") + 1], f"{base_revision}..HEAD")

    def test_invalid_comparison_revision_fails_closed(self) -> None:
        runner = Mock()
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT, base_revision="not-a-commit")

        self.assertEqual(result, 2)
        self.assertIn("comparison revision is invalid", output.getvalue())
        runner.assert_not_called()

    def test_valid_findings_show_only_bounded_safe_metadata(self) -> None:
        candidate = "ghp_" + "A" * 36
        finding = {
            "RuleID": "github-pat",
            "File": "backend/config/example.env",
            "StartLine": 12,
            "Commit": "a" * 40,
            "Secret": candidate,
            "Match": candidate,
        }
        runner = Mock(
            return_value=subprocess.CompletedProcess(
                args=[], returncode=1, stdout=json.dumps([finding]), stderr=candidate
            )
        )
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 1)
        self.assertIn("github-pat: backend/config/example.env:12", output.getvalue())
        self.assertNotIn(candidate, output.getvalue())
        self.assertNotIn("Match", output.getvalue())

    def test_valid_report_after_scanner_prefix_is_summarized_safely(self) -> None:
        candidate = "ghp_" + "A" * 36
        finding = {
            "RuleID": "github-pat",
            "File": "backend/config/example.env",
            "StartLine": 12,
            "Commit": "b" * 40,
            "Secret": candidate,
        }
        runner = Mock(
            return_value=subprocess.CompletedProcess(
                args=[],
                returncode=1,
                stdout="scanner warning\n" + json.dumps([finding]),
                stderr="",
            )
        )
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 1)
        self.assertIn("github-pat: backend/config/example.env:12", output.getvalue())
        self.assertNotIn(candidate, output.getvalue())

    def test_all_small_reports_are_shown_without_candidate_values(self) -> None:
        candidate = "synthetic-secret-value"
        findings = [
            {
                "RuleID": "generic-api-key",
                "File": f"backend/test-fixture-{index}.go",
                "StartLine": index + 1,
                "Commit": "c" * 40,
                "Secret": candidate,
            }
            for index in range(27)
        ]
        runner = Mock(
            return_value=subprocess.CompletedProcess(
                args=[], returncode=1, stdout=json.dumps(findings), stderr=candidate
            )
        )
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 1)
        self.assertIn("backend/test-fixture-26.go:27", output.getvalue())
        self.assertNotIn("additional finding(s) omitted", output.getvalue())
        self.assertNotIn(candidate, output.getvalue())

    def test_malformed_findings_remain_fully_withheld(self) -> None:
        candidate = "ghp_" + "A" * 36
        runner = Mock(
            return_value=subprocess.CompletedProcess(
                args=[], returncode=1, stdout=candidate, stderr=candidate
            )
        )
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 1)
        self.assertIn("not valid JSON", output.getvalue())
        self.assertNotIn(candidate, output.getvalue())

    def test_success_never_prints_captured_report(self) -> None:
        candidate = "synthetic-candidate-never-print"
        runner = Mock(
            return_value=subprocess.CompletedProcess(
                args=[], returncode=0, stdout=candidate, stderr=candidate
            )
        )
        output = io.StringIO()
        with redirect_stdout(output):
            result = secret_scan.scan(runner=runner, root=ROOT)

        self.assertEqual(result, 0)
        self.assertEqual(output.getvalue().strip(), "Secret scan passed.")
        self.assertNotIn(candidate, output.getvalue())


if __name__ == "__main__":
    unittest.main()
