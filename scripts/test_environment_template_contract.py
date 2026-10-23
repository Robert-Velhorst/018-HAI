#!/usr/bin/env python3
"""Check the canonical single-line example, never installed configuration files."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
ASSIGNMENT = re.compile(r"([A-Z][A-Z0-9_]*)=(.*)")


def parse_template(text: str) -> dict[str, str]:
    """Parse this repository's example format, not arbitrary dotenv syntax."""
    values: dict[str, str] = {}
    for number, line in enumerate(text.splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        match = ASSIGNMENT.fullmatch(line)
        if match is None:
            raise ValueError(f"Unsupported example declaration at line {number}")
        key, value = match.groups()
        if key in values:
            raise ValueError(f"Duplicate example key {key} at line {number}")
        values[key] = value
    return values


class EnvironmentTemplateContractTest(unittest.TestCase):
    def test_example_has_unique_supported_declarations(self):
        values = parse_template((ROOT / ".env.example").read_text(encoding="utf-8"))
        self.assertTrue(values)

    def test_sensitive_capabilities_remain_opt_in(self):
        values = parse_template((ROOT / ".env.example").read_text(encoding="utf-8"))
        for key in (
            "LOCAL_LOGIN_BYPASS_ENABLED",
            "HAI_A2A_BRIDGE_ENABLED",
            "DEEPSEEK_HARNESS_ENABLED",
            "DEEPSEEK_HARNESS_EXECUTION_ENABLED",
            "HAI_HOST_RUNTIME_BRIDGE_ENABLED",
            "SOURCE_SCHEDULER_VOLATILE_FALLBACK",
            "DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED",
            "SOURCE_MANUAL_WORKER_ENABLED",
            "HIRE_AI_CONNECTOR_ENABLED",
            "HAI_CHATGPT_LOGS_MCP_ENABLED",
        ):
            with self.subTest(key=key):
                # Do not echo a mistakenly populated credential or other value.
                self.assertTrue(values.get(key) == "false", f"{key} must default to false")

    def test_gateway_default_is_loopback(self):
        values = parse_template((ROOT / ".env.example").read_text(encoding="utf-8"))
        self.assertTrue(values.get("GATEWAY_HOST_BIND") == "127.0.0.1")

    def test_duplicate_error_does_not_expose_values(self):
        with self.assertRaises(ValueError) as caught:
            parse_template("EXAMPLE=first-private-value\nEXAMPLE=second-private-value\n")
        self.assertEqual(str(caught.exception), "Duplicate example key EXAMPLE at line 2")

    def test_malformed_declaration_error_does_not_expose_content(self):
        for line in ("export EXAMPLE=private-value", "example=private-value", "private-value"):
            with self.subTest(kind=line.split("=", 1)[0] if "=" in line else "bare"):
                with self.assertRaises(ValueError) as caught:
                    parse_template("# heading\n" + line)
                self.assertEqual(str(caught.exception), "Unsupported example declaration at line 2")

    def test_comments_blank_values_and_embedded_equals_are_supported(self):
        self.assertEqual(
            parse_template("# Heading\n \n  # Note\nEMPTY=\nEXAMPLE=a=b#c\n"),
            {"EMPTY": "", "EXAMPLE": "a=b#c"},
        )

    def test_line_endings_do_not_change_declarations(self):
        self.assertEqual(parse_template("EXAMPLE=a\r\nOTHER=b\r\n"),
                         parse_template("EXAMPLE=a\nOTHER=b\n"))


if __name__ == "__main__":
    unittest.main()
