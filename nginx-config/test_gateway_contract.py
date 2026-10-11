from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
NGINX_TEMPLATE = (ROOT / "nginx-config" / "nginx.conf.template").read_text(encoding="utf-8")
COMPOSE = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")

SECURITY_HEADERS = (
    'add_header X-Content-Type-Options "nosniff" always;',
    'add_header X-Frame-Options "DENY" always;',
    'add_header X-XSS-Protection "0" always;',
    'add_header Referrer-Policy "no-referrer" always;',
    'add_header Permissions-Policy "camera=(), microphone=(), geolocation=(), payment=(), usb=()" always;',
    'add_header Cross-Origin-Opener-Policy "same-origin" always;',
    'add_header Cross-Origin-Resource-Policy "same-origin" always;',
    "add_header Content-Security-Policy \"default-src 'self';",
)


def location_block(marker: str) -> str:
    start = NGINX_TEMPLATE.index(marker)
    brace = NGINX_TEMPLATE.index("{", start)
    depth = 0
    for index in range(brace, len(NGINX_TEMPLATE)):
        char = NGINX_TEMPLATE[index]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return NGINX_TEMPLATE[start : index + 1]
    raise AssertionError(f"unterminated nginx block: {marker}")


class GatewayAuthContractTest(unittest.TestCase):
    def test_gateway_is_the_single_public_security_header_authority(self) -> None:
        server = location_block("server {")
        for header in (
            "Content-Security-Policy",
            "Cross-Origin-Resource-Policy",
            "Referrer-Policy",
            "X-Content-Type-Options",
            "X-Frame-Options",
            "X-XSS-Protection",
        ):
            with self.subTest(header=header):
                self.assertIn(f"proxy_hide_header {header};", server)

    def test_gateway_applies_security_headers_to_every_response(self) -> None:
        server = location_block("server {")
        for required in SECURITY_HEADERS:
            with self.subTest(required=required):
                self.assertIn(required, server)
        self.assertIn("server_tokens off;", NGINX_TEMPLATE)

        # An add_header in a location disables inherited add_header values, so
        # locations that refresh the access cookie must repeat the baseline.
        for marker in (
            "location = /api/v1/agent-runtimes/openclaw/ecosystem/upload",
            "location /api/v1 {",
        ):
            block = location_block(marker)
            for required in SECURITY_HEADERS:
                with self.subTest(marker=marker, required=required):
                    self.assertIn(required, block)

    def test_backend_routes_use_authenticated_catch_all(self) -> None:
        backend = location_block("location /api/v1 {")
        self.assertIn("auth_request /auth-verify;", backend)
        self.assertIn("proxy_pass http://$backend_upstream;", backend)
        self.assertNotIn("location ~", backend)

    def test_protected_backend_routes_forward_verified_refreshed_identity(self) -> None:
        markers = (
            "location = /api/v1/agent-runtimes/openclaw/ecosystem/upload",
            "location /api/v1 {",
        )
        for marker in markers:
            with self.subTest(marker=marker):
                block = location_block(marker)
                self.assertIn("auth_request /auth-verify;", block)
                self.assertIn(
                    "auth_request_set $hai_refreshed_access_cookie $upstream_http_set_cookie;",
                    block,
                )
                self.assertIn(
                    "auth_request_set $hai_verified_access_token "
                    "$upstream_http_x_hai_verified_access_token;",
                    block,
                )
                self.assertIn(
                    'proxy_set_header Authorization "Bearer $hai_verified_access_token";',
                    block,
                )
                self.assertIn(
                    "add_header Set-Cookie $hai_refreshed_access_cookie always;",
                    block,
                )

    def test_auth_subrequest_is_internal_and_marks_itself(self) -> None:
        block = location_block("location /auth-verify")
        self.assertIn("internal;", block)
        self.assertIn('proxy_set_header X-HAI-Auth-Subrequest "1";', block)

    def test_a2a_connector_bypasses_browser_auth_but_keeps_narrow_token_boundary(self) -> None:
        card = location_block("location = /.well-known/agent-card.json")
        send = location_block("location = /api/v1/a2a")

        for block in (card, send):
            self.assertIn("limit_req zone=hai_a2a_bridge", block)
            self.assertIn("proxy_pass http://$backend_upstream;", block)
            self.assertIn("proxy_set_header Cookie \"\";", block)
            self.assertNotIn("auth_request /auth-verify;", block)

        self.assertIn("proxy_set_header Authorization \"\";", card)
        self.assertIn("proxy_set_header Authorization $http_authorization;", send)
        self.assertIn("proxy_set_header A2A-Version $http_a2a_version;", send)
        self.assertIn("limit_except POST { deny all; }", send)
        self.assertIn("client_max_body_size 16k;", send)

    def test_a2a_connector_has_dedicated_rate_limit_zone(self) -> None:
        self.assertIn(
            "limit_req_zone $binary_remote_addr zone=hai_a2a_bridge:1m rate=10r/m;",
            NGINX_TEMPLATE,
        )

    def test_public_identity_routes_are_resource_bounded(self) -> None:
        self.assertIn(
            "limit_req_zone $binary_remote_addr zone=hai_auth:1m rate=30r/m;",
            NGINX_TEMPLATE,
        )
        self.assertIn("limit_req_status 429;", NGINX_TEMPLATE)
        for marker in (
            "location ^~ /api/v1/auth/",
            "location ^~ /api/v1/user/",
        ):
            with self.subTest(marker=marker):
                self.assertIn(
                    "limit_req zone=hai_auth burst=10 nodelay;",
                    location_block(marker),
                )

    def test_read_only_identity_checks_do_not_exhaust_the_login_bucket(self) -> None:
        for marker in (
            "location = /api/v1/auth/session",
            "location = /api/v1/auth/capabilities",
        ):
            with self.subTest(marker=marker):
                block = location_block(marker)
                self.assertIn("limit_except GET { deny all; }", block)
                self.assertIn("proxy_pass http://$idp_upstream;", block)
                self.assertIn('proxy_set_header X-HAI-Auth-Subrequest "";', block)
                self.assertIn("proxy_hide_header X-HAI-Verified-Access-Token;", block)
                self.assertNotIn("limit_req zone=hai_auth", block)

    def test_gateway_has_one_authoritative_configuration(self) -> None:
        self.assertFalse((ROOT / "nginx-config" / "nginx.conf").exists())

    def test_idp_namespaces_cannot_request_or_receive_verified_token_header(self) -> None:
        for marker in (
            "location ^~ /api/v1/auth/",
            "location ^~ /api/v1/user/",
        ):
            with self.subTest(marker=marker):
                block = location_block(marker)
                self.assertIn("proxy_pass http://$idp_upstream;", block)
                self.assertIn('proxy_set_header X-HAI-Auth-Subrequest "";', block)
                self.assertIn("proxy_hide_header X-HAI-Verified-Access-Token;", block)
                self.assertNotIn("auth_request /auth-verify;", block)

    def test_compose_passes_the_same_gateway_bind_to_idp_and_gateway(self) -> None:
        self.assertIn(
            "GATEWAY_HOST_BIND: ${GATEWAY_HOST_BIND:-127.0.0.1}",
            COMPOSE,
        )
        self.assertIn(
            '"${GATEWAY_HOST_BIND:-127.0.0.1}:${GATEWAY_HOST_PORT:-8088}:8080"',
            COMPOSE,
        )

    def test_gateway_runtime_is_non_root_read_only_and_resource_bounded(self) -> None:
        gateway_start = COMPOSE.index("  nginx:\n")
        gateway_end = COMPOSE.index("\n  ngrok:\n", gateway_start)
        gateway = COMPOSE[gateway_start:gateway_end]

        for required in (
            "nginx:stable-alpine-slim@sha256:",
            'user: "101:101"',
            "init: true",
            "read_only: true",
            "/tmp:rw,noexec,nosuid,nodev,size=${GATEWAY_TMPFS_SIZE:-32m}",
            "mem_limit: ${GATEWAY_MEMORY_LIMIT:-64m}",
            "mem_reservation: ${GATEWAY_MEMORY_RESERVATION:-16m}",
            "cpus: ${GATEWAY_CPU_LIMIT:-0.25}",
            "pids_limit: ${GATEWAY_PIDS_LIMIT:-32}",
            "no-new-privileges:true",
            "cap_drop:",
            "- ALL",
            'entrypoint: ["/bin/sh", "-c"]',
            "> /tmp/nginx.conf",
            "nginx -c /tmp/nginx.conf",
            "http://127.0.0.1:8080/healthz",
        ):
            with self.subTest(required=required):
                self.assertIn(required, gateway)

        for required in (
            "worker_processes 1;",
            "pid /tmp/nginx.pid;",
            "access_log /dev/stdout;",
            "proxy_temp_path /tmp/proxy_temp;",
            "listen 8080;",
        ):
            with self.subTest(config_required=required):
                self.assertIn(required, NGINX_TEMPLATE)


if __name__ == "__main__":
    unittest.main()
