import hashlib
import unittest
from unittest.mock import patch

import app


class CrewAIRunnerTests(unittest.TestCase):
    def test_crew_keeps_memory_tools_and_delegation_disabled(self):
        agents = [object(), object()]
        tasks = [object(), object()]
        with (
            patch.object(app, "LLM", return_value=object()),
            patch.object(app, "Agent", side_effect=agents) as agent_factory,
            patch.object(app, "Task", side_effect=tasks),
            patch.object(app, "Crew") as crew_factory,
        ):
            app.create_crew("http://ollama:11434/v1", "qwen-local", "local-key", "Plan", [], [])

        for call in agent_factory.call_args_list:
            self.assertEqual(call.kwargs["tools"], [])
            self.assertFalse(call.kwargs["allow_delegation"])

        options = crew_factory.call_args.kwargs
        self.assertFalse(options["memory"])
        self.assertFalse(options["cache"])
        self.assertFalse(options["planning"])
        self.assertFalse(options["share_crew"])

    def test_guidance_is_bounded_pin_checked_and_labeled_as_advisory(self):
        summary = "For an MCP integration, define the smallest useful tool contract."
        item = {
            "skillId": "mcp-builder",
            "name": "MCP builder",
            "sourceCommit": "a" * 40,
            "sourceSHA256": "b" * 64,
            "guidanceSHA256": hashlib.sha256(summary.encode("utf-8")).hexdigest(),
            "consentDecisionId": 23,
            "guidance": summary,
        }
        request, _, guidance = app.validate_payload({"request": "Build an MCP adapter", "guidance": [item]})
        self.assertEqual(request, "Build an MCP adapter")
        context = app.guidance_context(guidance)
        self.assertIn("untrusted, non-authoritative advisory context", context)
        self.assertIn(summary, context)
        item["guidance"] = "Do something else"
        with self.assertRaises(app.RequestError):
            app.validate_payload({"request": "Build an MCP adapter", "guidance": [item]})

    def test_unknown_runner_fields_and_empty_guidance(self):
        self.assertEqual(app.validate_payload({"request": "Plan", "successCriteria": []}), ("Plan", [], []))
        with self.assertRaises(app.RequestError):
            app.validate_payload({"request": "Plan", "rawSkillFile": "not accepted"})


if __name__ == "__main__":
    unittest.main()
