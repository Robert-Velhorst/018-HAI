from pathlib import Path
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

import app


class GitleaksRunnerContractTest(unittest.TestCase):
    def test_request_accepts_only_a_bounded_snapshot_name(self):
        self.assertEqual(app.request_workspace(b'{"workspaceId":"approved_01"}'), "approved_01")
        for payload in (
            b'{"workspaceId":"../outside"}',
            b'{"workspaceId":""}',
            b'{"workspaceId":42}',
            b"not-json",
        ):
            with self.subTest(payload=payload), self.assertRaises(app.RequestError):
                app.request_workspace(payload)

    def test_configuration_fails_closed_without_token_or_allowlist(self):
        with TemporaryDirectory() as directory:
            with patch.dict(
                "os.environ",
                {
                    "HAI_GITLEAKS_RUNNER_TOKEN": "short",
                    "HAI_GITLEAKS_INPUT_ROOT": directory,
                    "HAI_GITLEAKS_WORKSPACES": "approved",
                },
                clear=True,
            ):
                with self.assertRaises(app.RequestError):
                    app.configured()

            with patch.dict(
                "os.environ",
                {
                    "HAI_GITLEAKS_RUNNER_TOKEN": "synthetic-test-token-0123456789",
                    "HAI_GITLEAKS_INPUT_ROOT": directory,
                    "HAI_GITLEAKS_WORKSPACES": "",
                },
                clear=True,
            ):
                with self.assertRaises(app.RequestError):
                    app.configured()


if __name__ == "__main__":
    unittest.main()
