import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading
import unittest
from unittest.mock import patch

import app


class LocalPlannerTests(unittest.TestCase):
    def proposal(self, goal):
        return {
            "goal": goal,
            "successCriteria": ["The plan is bounded"],
            "nextSteps": ["Review the proposed steps"],
            "risk": "low",
            "requiresApproval": True,
            "reasons": ["External effects need review"],
            "uncertainties": [],
        }

    def test_propose_runs_planner_then_safety_review_without_tools(self):
        candidate = self.proposal("Create a review-only plan")
        final = self.proposal("Create a reviewed, bounded plan")
        with (
            patch.object(app, "configured", return_value=("http://ollama:11434/v1", "qwen-local", "local-key")),
            patch.object(app, "chat_completion", side_effect=[json.dumps(candidate), json.dumps(final)]) as complete,
        ):
            response = app.propose({"request": "Plan a local documentation update", "successCriteria": ["Do not publish"]})

        self.assertEqual(response["engine"], app.MODEL_ENGINE)
        self.assertEqual(response["proposal"], final)
        self.assertEqual(complete.call_count, 2)
        planner_messages = complete.call_args_list[0].args[3]
        reviewer_messages = complete.call_args_list[1].args[3]
        self.assertEqual([item["role"] for item in planner_messages], ["system", "user"])
        self.assertIn("untrusted input", planner_messages[1]["content"])
        self.assertEqual([item["role"] for item in reviewer_messages], ["system", "user"])
        self.assertIn('"candidatePlan"', reviewer_messages[1]["content"])

    def test_invalid_planner_candidate_stops_before_review(self):
        with (
            patch.object(app, "configured", return_value=("http://ollama:11434/v1", "qwen-local", "local-key")),
            patch.object(app, "chat_completion", return_value="{}") as complete,
        ):
            with self.assertRaises(app.RequestError):
                app.propose({"request": "Plan"})

        self.assertEqual(complete.call_count, 1)

    def test_chat_completion_uses_the_configured_local_openai_compatible_endpoint(self):
        class ModelHandler(BaseHTTPRequestHandler):
            seen = None

            def do_POST(self):
                self.__class__.seen = {
                    "path": self.path,
                    "authorization": self.headers.get("Authorization"),
                    "body": json.loads(self.rfile.read(int(self.headers["Content-Length"]))),
                }
                response = json.dumps({"choices": [{"message": {"content": json.dumps({"ok": True})}}]}).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(response)))
                self.end_headers()
                self.wfile.write(response)

            def log_message(self, _format, *_args):
                return

        server = ThreadingHTTPServer(("127.0.0.1", 0), ModelHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            base_url = f"http://127.0.0.1:{server.server_port}/v1"
            content = app.chat_completion(base_url, "local-model", "secret-value", [{"role": "user", "content": "Plan"}])
        finally:
            server.shutdown()
            thread.join(timeout=2)
            server.server_close()

        self.assertEqual(json.loads(content), {"ok": True})
        self.assertEqual(ModelHandler.seen["path"], "/v1/chat/completions")
        self.assertEqual(ModelHandler.seen["authorization"], "Bearer secret-value")
        self.assertEqual(ModelHandler.seen["body"]["model"], "local-model")
        self.assertEqual(ModelHandler.seen["body"]["temperature"], 0)
        self.assertEqual(ModelHandler.seen["body"]["stream"], False)

    def test_chat_completion_does_not_follow_redirects(self):
        class RedirectTargetHandler(BaseHTTPRequestHandler):
            requests_seen = 0

            def do_POST(self):
                self.__class__.requests_seen += 1
                self.send_response(200)
                self.end_headers()

            def log_message(self, _format, *_args):
                return

        target = ThreadingHTTPServer(("127.0.0.1", 0), RedirectTargetHandler)
        target_thread = threading.Thread(target=target.serve_forever, daemon=True)
        target_thread.start()

        class RedirectHandler(BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(307)
                self.send_header("Location", f"http://127.0.0.1:{target.server_port}/collect")
                self.end_headers()

            def log_message(self, _format, *_args):
                return

        source = ThreadingHTTPServer(("127.0.0.1", 0), RedirectHandler)
        source_thread = threading.Thread(target=source.serve_forever, daemon=True)
        source_thread.start()
        try:
            with self.assertRaises(app.RequestError):
                app.chat_completion(
                    f"http://127.0.0.1:{source.server_port}/v1",
                    "local-model",
                    "secret-value",
                    [{"role": "user", "content": "private planning input"}],
                )
        finally:
            source.shutdown()
            source_thread.join(timeout=2)
            source.server_close()
            target.shutdown()
            target_thread.join(timeout=2)
            target.server_close()

        self.assertEqual(RedirectTargetHandler.requests_seen, 0)

    def test_probe_does_not_follow_redirects(self):
        class RedirectTargetHandler(BaseHTTPRequestHandler):
            requests_seen = 0

            def do_GET(self):
                self.__class__.requests_seen += 1
                self.send_response(200)
                self.end_headers()

            def log_message(self, _format, *_args):
                return

        target = ThreadingHTTPServer(("127.0.0.1", 0), RedirectTargetHandler)
        target_thread = threading.Thread(target=target.serve_forever, daemon=True)
        target_thread.start()

        class RedirectHandler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(302)
                self.send_header("Location", f"http://127.0.0.1:{target.server_port}/models")
                self.end_headers()

            def log_message(self, _format, *_args):
                return

        source = ThreadingHTTPServer(("127.0.0.1", 0), RedirectHandler)
        source_thread = threading.Thread(target=source.serve_forever, daemon=True)
        source_thread.start()
        try:
            with (
                patch.object(
                    app,
                    "configured",
                    return_value=(f"http://127.0.0.1:{source.server_port}/v1", "local-model", "secret-value"),
                ),
                self.assertRaises(app.RequestError),
            ):
                app.probe()
        finally:
            source.shutdown()
            source_thread.join(timeout=2)
            source.server_close()
            target.shutdown()
            target_thread.join(timeout=2)
            target.server_close()

        self.assertEqual(RedirectTargetHandler.requests_seen, 0)

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
