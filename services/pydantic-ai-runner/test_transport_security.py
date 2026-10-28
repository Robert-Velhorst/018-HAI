"""Owned loopback tests of the actual probe, SDK transport and proposal lifecycle."""
import json
import os
import threading
import unittest
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

import app
from openai import APIStatusError
from pydantic_ai.exceptions import ModelHTTPError

OBSERVATIONS = []
MODEL = 'owned-fixture-model'
KEY = 'owned-fixture-key-not-a-credential'
PROMPT = 'owned private planning payload'
PROPOSAL = {'goal': 'Review the owned fixture', 'successCriteria': ['Fixture reviewed'],
            'nextSteps': ['Inspect the fixture'], 'risk': 'low', 'requiresApproval': False,
            'reasons': ['Owned test'], 'uncertainties': []}


@contextmanager
def endpoint(label, *, redirect=None, status=200):
    seen = []
    class Handler(BaseHTTPRequestHandler):
        def respond(self):
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 <= size <= 64 * 1024:
                self.send_error(413)
                return
            raw = self.rfile.read(size)
            seen.append({'method': self.command, 'path': self.path,
                         'fixture_authorization': self.headers.get('Authorization') == 'Bearer ' + KEY,
                         'private_prompt_in_body': PROMPT.encode() in raw})
            if redirect is not None:
                self.send_response(status)
                self.send_header('Location', redirect)
                self.send_header('Content-Length', '0')
                self.end_headers()
                return
            if self.command == 'GET':
                result = {'object': 'list', 'data': [{'id': MODEL, 'object': 'model', 'created': 0, 'owned_by': 'fixture'}]}
            else:
                body = json.loads(raw)
                tools = body.get('tools', [])
                if tools:
                    message = {'role': 'assistant', 'content': None, 'tool_calls': [
                        {'id': 'owned-call-1', 'type': 'function', 'function': {
                            'name': tools[-1]['function']['name'], 'arguments': json.dumps(PROPOSAL)}}]}
                    finish = 'tool_calls'
                else:
                    message = {'role': 'assistant', 'content': json.dumps(PROPOSAL)}
                    finish = 'stop'
                result = {'id': 'owned-chat-1', 'object': 'chat.completion', 'created': 0, 'model': MODEL,
                          'choices': [{'index': 0, 'message': message, 'finish_reason': finish}],
                          'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}
            data = json.dumps(result).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        do_GET = respond
        do_POST = respond
        def log_message(self, *_args):
            pass
    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f'http://127.0.0.1:{server.server_port}', seen
    finally:
        server.shutdown()
        thread.join(timeout=3)
        server.server_close()
        assert not thread.is_alive()
        OBSERVATIONS.append({'label': label, 'requests': seen})


@contextmanager
def environment(base):
    with patch.dict(os.environ, {'HAI_PYDANTIC_AI_LOCAL_BASE_URL': base + '/v1',
                                'HAI_PYDANTIC_AI_LOCAL_MODEL_ID': MODEL,
                                'HAI_PYDANTIC_AI_LOCAL_API_KEY': KEY,
                                'NO_PROXY': '*', 'no_proxy': '*'}, clear=True):
        yield


@contextmanager
def captured_clients():
    actual = app.AsyncOpenAI
    clients = []
    def create(*args, **kwargs):
        client = actual(*args, **kwargs)
        clients.append(client)
        return client
    with patch.object(app, 'AsyncOpenAI', side_effect=create):
        yield clients


class LocalTransportSecurityTests(unittest.TestCase):
    def probe_redirect(self, status):
        with endpoint(f'probe-{status}-target') as (target, target_seen):
            with endpoint(f'probe-{status}-source', redirect=target + '/models', status=status) as (source, source_seen):
                with environment(source):
                    with self.assertRaises(app.RequestError):
                        app.probe()
                self.assertEqual(len(source_seen), 1)
                self.assertEqual(target_seen, [])

    def test_probe_301_never_contacts_redirect_target(self): self.probe_redirect(301)
    def test_probe_302_never_contacts_redirect_target(self): self.probe_redirect(302)
    def test_probe_303_never_contacts_redirect_target(self): self.probe_redirect(303)
    def test_probe_307_never_contacts_redirect_target(self): self.probe_redirect(307)
    def test_probe_308_never_contacts_redirect_target(self): self.probe_redirect(308)

    def test_direct_probe_keeps_model_and_authorization(self):
        with endpoint('direct-probe') as (source, seen):
            with environment(source):
                result = app.probe()
            self.assertEqual(result['status'], 'ok')
            self.assertEqual(result['modelId'], MODEL)
            self.assertEqual(seen, [{'method': 'GET', 'path': '/v1/models',
                                    'fixture_authorization': True, 'private_prompt_in_body': False}])

    def completion_redirect(self, status):
        with endpoint(f'completion-{status}-target') as (target, target_seen):
            with endpoint(f'completion-{status}-source', redirect=target + '/collect', status=status) as (source, source_seen):
                with environment(source), captured_clients() as clients:
                    with self.assertRaises((APIStatusError, ModelHTTPError)) as raised:
                        app.propose({'request': PROMPT, 'successCriteria': []})
                self.assertEqual(raised.exception.status_code, status)
                self.assertEqual(len(source_seen), 1)
                self.assertTrue(source_seen[0]['private_prompt_in_body'])
                self.assertEqual(target_seen, [])
                self.assertEqual(len(clients), 1)
                self.assertTrue(clients[0].is_closed())

    def test_real_sdk_307_never_forwards_planning_body(self): self.completion_redirect(307)
    def test_real_sdk_308_never_forwards_planning_body(self): self.completion_redirect(308)

    def test_direct_proposal_preserves_real_agent_schema_and_prompt(self):
        with endpoint('direct-proposal') as (source, seen):
            with environment(source):
                result = app.propose({'request': PROMPT, 'successCriteria': []})
            self.assertEqual(result['proposal'], PROPOSAL)
            self.assertEqual(result['modelId'], MODEL)
            self.assertEqual(len(seen), 1)
            self.assertEqual(seen[0]['path'], '/v1/chat/completions')
            self.assertTrue(seen[0]['fixture_authorization'])
            self.assertTrue(seen[0]['private_prompt_in_body'])

    def test_request_owned_client_closes_after_success(self):
        with endpoint('successful-client-lifetime') as (source, seen):
            with environment(source), captured_clients() as clients:
                result = app.propose({'request': PROMPT, 'successCriteria': []})
            self.assertEqual(result['proposal'], PROPOSAL)
            self.assertEqual(len(seen), 1)
            self.assertEqual(len(clients), 1)
            self.assertTrue(clients[0].is_closed())


def tearDownModule():
    Path('owned-transport-observations.json').write_text(json.dumps(OBSERVATIONS, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    unittest.main()
