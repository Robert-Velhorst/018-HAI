"""Owned real-Garak loopback regressions, separate from unchanged vendor suites."""
import importlib
from importlib.metadata import version
import json
import os
import threading
import unittest
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

import app

OBSERVATIONS = []
MODEL = 'owned-local-fixture'
KEY = 'owned-fixture-not-a-live-credential'

@contextmanager
def endpoint(label, redirect_status=None, redirect_target=None):
    seen = []
    class Handler(BaseHTTPRequestHandler):
        def respond(self):
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 <= size <= 128 * 1024:
                self.send_error(413)
                return
            raw = self.rfile.read(size)
            body = json.loads(raw) if raw else {}
            seen.append({'method': self.command, 'path': self.path,
                         'fixture_authorization': self.headers.get('Authorization') == 'Bearer ' + KEY,
                         'planning_body_bytes': len(raw), 'contains_messages': bool(body.get('messages')),
                         'model': body.get('model'), 'temperature': body.get('temperature'),
                         'top_p': body.get('top_p'), 'max_tokens': body.get('max_tokens')})
            redirected = redirect_status is not None and (
                self.command == 'POST' or self.path.endswith('/models'))
            if redirected:
                self.send_response(redirect_status)
                self.send_header('Location', redirect_target)
                self.send_header('Content-Length', '0')
                self.end_headers()
                return
            if self.command == 'GET':
                value = {'object': 'list', 'data': [{'id': MODEL, 'object': 'model', 'created': 0, 'owned_by': 'fixture'}]}
            else:
                value = {'id': 'owned-completion', 'object': 'chat.completion', 'created': 0, 'model': MODEL,
                         'choices': [{'index': 0, 'message': {'role': 'assistant', 'content': 'Safe owned fixture response.'}, 'finish_reason': 'stop'}],
                         'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}
            data = json.dumps(value).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        do_GET = respond
        do_POST = respond
        def log_message(self, *_args): pass
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
def environment(source):
    # Keep the actual CLI executable and its normal system search path.
    values = {'PATH': os.environ.get('PATH', '/usr/local/bin:/usr/bin:/bin'),
              'HAI_GARAK_MODEL_BASE_URL': source + '/v1', 'HAI_GARAK_MODEL_ID': MODEL,
              'HAI_GARAK_MODEL_API_KEY': KEY, 'HOME': '/tmp',
              'NO_PROXY': '*', 'no_proxy': '*', 'PYTHONDONTWRITEBYTECODE': '1'}
    with patch.dict(os.environ, values, clear=True): yield

class GarakTransportSecurityTests(unittest.TestCase):
    def probe_redirect(self, status):
        with endpoint(f'probe-{status}-target') as (target, target_seen):
            with endpoint(f'probe-{status}-source', status, target + '/models') as (source, source_seen):
                with environment(source), self.assertRaises(app.RequestError): app.probe()
                self.assertEqual(len(source_seen), 1)
                self.assertEqual(target_seen, [])
    def test_probe_301(self): self.probe_redirect(301)
    def test_probe_302(self): self.probe_redirect(302)
    def test_probe_303(self): self.probe_redirect(303)
    def test_probe_307(self): self.probe_redirect(307)
    def test_probe_308(self): self.probe_redirect(308)

    def test_direct_probe_preserves_model_and_authorization(self):
        with endpoint('direct-probe') as (source, seen):
            with environment(source): result = app.probe()
            self.assertEqual(result['status'], 'ok')
            self.assertEqual(result['modelId'], MODEL)
            self.assertEqual(len(seen), 1)
            self.assertTrue(seen[0]['fixture_authorization'])

    def cli_redirect(self, status):
        with endpoint(f'cli-{status}-target') as (target, target_seen):
            with endpoint(f'cli-{status}-source', status, target + '/collect') as (source, source_seen):
                with environment(source), self.assertRaises(app.RequestError): app.run_scan()
                self.assertTrue(any(row['method'] == 'POST' for row in source_seen), 'The actual CLI must reach the model')
                self.assertEqual(target_seen, [])
    def test_actual_cli_307_fails_closed_without_forwarding(self): self.cli_redirect(307)
    def test_actual_cli_308_fails_closed_without_forwarding(self): self.cli_redirect(308)

    def test_actual_cli_direct_control_preserves_fixed_suite_and_parameters(self):
        with endpoint('direct-cli') as (source, seen):
            with environment(source): result = app.run_scan()
            self.assertEqual(result['suite'], app.SUITE)
            self.assertEqual(result['modelId'], MODEL)
            self.assertTrue(1 <= result['caseCount'] <= app.MAX_CASES)
            posts = [row for row in seen if row['method'] == 'POST']
            self.assertTrue(posts, 'The actual CLI must issue completions')
            self.assertTrue(all(row['fixture_authorization'] and row['contains_messages'] for row in posts))
            self.assertTrue(all(row['model'] == MODEL and row['temperature'] == 0 and row['top_p'] == 1.0 and row['max_tokens'] == 64 for row in posts))

    def test_actual_generator_recreation_preserves_policy_and_cleanup(self):
        module_name, classes = next(iter(app.run_config('http://127.0.0.1:1/v1')['plugins']['generators'].items()))
        class_name = next(iter(classes))
        module = importlib.import_module('garak.generators.' + module_name)
        cls = getattr(module, class_name)
        generator = cls.__new__(cls)
        generator.name = MODEL
        generator.uri = 'http://127.0.0.1:1/v1'
        generator.api_key = KEY
        try:
            generator._load_unsafe()
            self.assertFalse(generator.client._client.follow_redirects)
            client = generator.client
            generator.close()
            self.assertTrue(client.is_closed())
            self.assertIsNone(generator.client)
            self.assertIsNone(generator.generator)
            generator._load_unsafe()
            self.assertFalse(generator.client._client.follow_redirects)
        finally:
            generator.close()

def tearDownModule():
    print('OWNED_GARAK_OBSERVATIONS=' + json.dumps({'garak': version('garak'), 'openai': version('openai'),
          'dependency_graph_profile': 'inherited partial profile; full SDK graph unqualified', 'observations': OBSERVATIONS}))

if __name__ == '__main__': unittest.main()
