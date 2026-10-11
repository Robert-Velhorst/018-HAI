from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
NGINX_TEMPLATE = (ROOT / "nginx-config" / "nginx.conf.template").read_text(encoding="utf-8")
NGINX_CONFIG = (ROOT / "nginx-config" / "nginx.conf").read_text(encoding="utf-8")
COMPOSE = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
LEGACY_COMPOSE = (ROOT / "docker-compose.yml").read_text(encoding="utf-8")


def location_block(marker: str, config: str = NGINX_TEMPLATE) -> str:
    start = config.index(marker)
    brace = config.index("{", start)
    depth = 0
    for index in range(brace, len(config)):
        char = config[index]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return config[start : index + 1]
    raise AssertionError(f"unterminated nginx block: {marker}")


def compose_service_block(name: str) -> str:
    lines = COMPOSE.splitlines()
    marker = f"  {name}:"
    start = lines.index(marker)
    end = next(
        (
            index
            for index in range(start + 1, len(lines))
            if lines[index].startswith("  ") and not lines[index].startswith("    ") and lines[index].strip()
        ),
        len(lines),
    )
    return "\n".join(lines[start:end])


class GatewayAuthContractTest(unittest.TestCase):
    def test_gateway_renders_secret_bearing_config_with_restrictive_permissions(self) -> None:
        gateway = compose_service_block("nginx")
        umask = gateway.index("umask 077")
        render = gateway.index("envsubst '$$BACKEND_API_SHARED_KEY $$LOCAL_PREVIEW_GATEWAY_SECRET'")
        self.assertLess(umask, render)

    def test_gateway_container_health_requires_backend_and_idp_readiness(self) -> None:
        gateway = compose_service_block("nginx")
        self.assertIn("http://127.0.0.1/readyz", gateway)
        self.assertIn("http://127.0.0.1/_hai/idp-readyz", gateway)
        self.assertIn("-O /dev/null", gateway)
        self.assertNotIn("--spider", gateway)
        self.assertIn("--timeout=3", gateway)
        self.assertIn("timeout: 8s", gateway)
        self.assertNotIn("wget -q --spider http://127.0.0.1/ ", gateway)

    def test_gateway_hides_nginx_version_without_changing_local_surface(self) -> None:
        self.assertIn("server_tokens off;", NGINX_TEMPLATE)
        self.assertIn("server_tokens off;", NGINX_CONFIG)

        gateway = compose_service_block("nginx")
        self.assertIn('"127.0.0.1:${GATEWAY_HOST_PORT:-8088}:80"', gateway)
        self.assertIn("./nginx-config/nginx.conf.template:/etc/nginx/nginx.conf.template:ro", gateway)
        self.assertIn("./nginx-config/sites-enabled:/etc/nginx/sites-enabled:ro", gateway)

    def test_idp_readiness_probes_the_real_upstream_with_bounded_timeouts(self) -> None:
        readiness = location_block("location = /_hai/idp-readyz")
        self.assertIn("proxy_pass http://$idp_upstream/readyz;", readiness)
        self.assertIn("proxy_connect_timeout 2s;", readiness)
        self.assertIn("proxy_read_timeout 3s;", readiness)
        self.assertIn("proxy_send_timeout 3s;", readiness)
        self.assertNotIn("return 200", readiness)
        self.assertNotIn("auth_request", readiness)

        backend_readiness = location_block("location = /readyz")
        self.assertIn("proxy_pass http://$backend_upstream;", backend_readiness)
        self.assertNotIn("auth_request", backend_readiness)

    def test_direct_nginx_config_retains_the_safe_gateway_contract(self) -> None:
        upload = location_block(
            "location = /api/v1/agent-runtimes/openclaw/ecosystem/upload",
            NGINX_CONFIG,
        )
        backend = location_block("location /api/v1 {", NGINX_CONFIG)
        auth = location_block("location /auth-verify", NGINX_CONFIG)
        self.assertIn("auth_request /auth-verify;", upload)
        self.assertIn("client_max_body_size 752m;", upload)
        self.assertIn('proxy_set_header Authorization "Bearer $hai_verified_access_token";', upload)
        self.assertIn("auth_request /auth-verify;", backend)
        self.assertIn('proxy_set_header Authorization "Bearer $hai_verified_access_token";', backend)
        self.assertIn("internal;", auth)
        self.assertNotIn("location ~ ^/api/v1/", NGINX_CONFIG)

    def test_openclaw_upload_gateway_limit_matches_backend_envelope(self) -> None:
        block = location_block("location = /api/v1/agent-runtimes/openclaw/ecosystem/upload")
        self.assertIn("client_max_body_size 752m;", block)
        self.assertIn("proxy_request_buffering off;", block)
        self.assertIn("proxy_read_timeout 15m;", block)
        self.assertIn("proxy_send_timeout 15m;", block)

    def test_backend_routes_use_authenticated_catch_all(self) -> None:
        backend = location_block("location /api/v1 {")
        self.assertIn("auth_request /auth-verify;", backend)
        self.assertIn("proxy_pass http://$backend_upstream;", backend)
        self.assertNotIn("location ~", backend)

    def test_compatibility_and_generated_routes_propagate_rotated_cookies(self) -> None:
        static_route = (ROOT / "nginx-config" / "sites-enabled" / "generic-auto.conf").read_text(
            encoding="utf-8"
        )
        generated_route = (
            ROOT / "nginx-config-manager" / "internal" / "app" / "autoconfig" / "config_template.go"
        ).read_text(encoding="utf-8")
        for route in (static_route, generated_route):
            self.assertIn("auth_request /auth-verify;", route)
            self.assertIn("$upstream_http_x_hai_refreshed_access_cookie", route)
            self.assertIn("$upstream_http_x_hai_refreshed_refresh_cookie", route)
            self.assertIn("add_header Set-Cookie $hai_refreshed_access_cookie always;", route)
            self.assertIn("add_header Set-Cookie $hai_refreshed_refresh_cookie always;", route)

    def test_host_runtime_bridge_is_not_exposed_by_dashboard_gateway(self) -> None:
        bridge = location_block("location ^~ /api/v1/host-runtime/")
        self.assertIn("return 404;", bridge)
        self.assertNotIn("proxy_pass", bridge)

        host_gateway = (ROOT / "nginx-config" / "host-runtime.conf.template").read_text(
            encoding="utf-8"
        )
        self.assertIn('listen 8080;', host_gateway)
        self.assertIn('location = /api/v1/host-runtime/leases', host_gateway)
        self.assertIn('location ^~ /api/v1/host-runtime/leases/', host_gateway)
        self.assertIn('proxy_set_header Authorization $http_authorization;', host_gateway)
        self.assertIn('proxy_set_header Cookie "";', host_gateway)
        self.assertIn('proxy_set_header X-HAI-Auth-Subrequest "";', host_gateway)
        self.assertIn('proxy_set_header X-HAI-Verified-Access-Token "";', host_gateway)
        self.assertNotIn("auth_request", host_gateway)

    def test_local_agent_bridge_protocols_are_not_exposed_by_dashboard_gateway(self) -> None:
        for config_name, config in (("template", NGINX_TEMPLATE), ("direct", NGINX_CONFIG)):
            for marker in (
                "location = /api/v1/a2a",
                "location ^~ /api/v1/a2a/",
                "location ^~ /api/v1/mcp-agent/",
            ):
                with self.subTest(config=config_name, marker=marker):
                    bridge = location_block(marker, config)
                    self.assertIn("return 404;", bridge)
                    self.assertNotIn("proxy_pass", bridge)

    def test_optional_agent_bridges_remain_loopback_only_and_narrow(self) -> None:
        a2a_gateway = (ROOT / "nginx-config" / "a2a-local.conf.template").read_text(
            encoding="utf-8"
        )
        agent_card = location_block(
            "location = /.well-known/agent-card.json", a2a_gateway
        )
        send_message = location_block("location = /api/v1/a2a", a2a_gateway)
        fallback = location_block("location / {", a2a_gateway)
        self.assertIn("proxy_pass http://$backend_upstream;", agent_card)
        self.assertIn("proxy_pass http://$backend_upstream;", send_message)
        self.assertIn("proxy_set_header Authorization $http_authorization;", send_message)
        self.assertIn("return 404;", fallback)

        self.assertIn('profiles: ["local-a2a"]', COMPOSE)
        self.assertIn('"127.0.0.1:${HAI_A2A_LOCAL_PORT:-8091}:8080"', COMPOSE)
        self.assertIn('profiles: ["mcp-bridge"]', COMPOSE)
        self.assertIn('"127.0.0.1:${HAI_FASTMCP_PORT:-8090}:8080"', COMPOSE)

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
                    "auth_request_set $hai_refreshed_access_cookie "
                    "$upstream_http_x_hai_refreshed_access_cookie;",
                    block,
                )
                self.assertIn(
                    "auth_request_set $hai_refreshed_refresh_cookie "
                    "$upstream_http_x_hai_refreshed_refresh_cookie;",
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
                self.assertIn(
                    "add_header Set-Cookie $hai_refreshed_refresh_cookie always;",
                    block,
                )

    def test_auth_subrequest_is_internal_and_marks_itself(self) -> None:
        block = location_block("location /auth-verify")
        self.assertIn("internal;", block)
        self.assertIn('proxy_set_header X-HAI-Auth-Subrequest "1";', block)

    def test_idp_locations_overwrite_client_ip_forwarding_headers(self) -> None:
        configurations = (
            (
                "template",
                NGINX_TEMPLATE,
                (
                    "location = /api/v1/auth/local-preview",
                    "location ^~ /api/v1/auth/",
                    "location ^~ /api/v1/user/",
                    "location /auth-verify",
                ),
            ),
            (
                "direct",
                NGINX_CONFIG,
                (
                    "location ^~ /api/v1/auth/",
                    "location ^~ /api/v1/user/",
                    "location /auth-verify",
                ),
            ),
        )
        for config_name, config, markers in configurations:
            for marker in markers:
                with self.subTest(config=config_name, marker=marker):
                    block = location_block(marker, config)
                    self.assertIn("proxy_set_header X-Forwarded-For $remote_addr;", block)
                    self.assertIn("proxy_set_header X-Real-IP $remote_addr;", block)
                    self.assertIn('proxy_set_header Forwarded "";', block)
                    self.assertNotIn("$proxy_add_x_forwarded_for", block)
                    self.assertNotIn("$http_x_forwarded_for", block)

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
                self.assertIn("proxy_hide_header X-HAI-Refreshed-Access-Cookie;", block)
                self.assertIn("proxy_hide_header X-HAI-Refreshed-Refresh-Cookie;", block)
                self.assertNotIn("auth_request /auth-verify;", block)

    def test_local_preview_uses_exact_gateway_only_secret_injection(self) -> None:
        preview = location_block("location = /api/v1/auth/local-preview")
        auth = location_block("location ^~ /api/v1/auth/")
        gateway = compose_service_block("nginx")
        idp = compose_service_block("idp")
        backend = compose_service_block("backend")

        self.assertIn("proxy_pass http://$idp_upstream;", preview)
        self.assertIn("proxy_set_header Host $host;", preview)
        self.assertIn("proxy_set_header Origin $http_origin;", preview)
        self.assertIn(
            'proxy_set_header X-HAI-Local-Preview-Gateway-Secret "${LOCAL_PREVIEW_GATEWAY_SECRET}";',
            preview,
        )
        self.assertIn("proxy_hide_header X-HAI-Refreshed-Access-Cookie;", preview)
        self.assertIn("proxy_hide_header X-HAI-Refreshed-Refresh-Cookie;", preview)
        self.assertNotIn("LOCAL_PREVIEW_GATEWAY_SECRET", auth)
        self.assertIn("LOCAL_PREVIEW_GATEWAY_SECRET: ${LOCAL_PREVIEW_GATEWAY_SECRET:-}", gateway)
        self.assertIn("LOCAL_PREVIEW_GATEWAY_SECRET: ${LOCAL_PREVIEW_GATEWAY_SECRET:-}", idp)
        self.assertNotIn("LOCAL_PREVIEW_GATEWAY_SECRET", backend)
        self.assertIn("envsubst '$$BACKEND_API_SHARED_KEY $$LOCAL_PREVIEW_GATEWAY_SECRET'", gateway)
        self.assertIn("*[!0-9a-f]*", gateway)

    def test_gateway_is_loopback_only_while_idp_receives_validated_bind(self) -> None:
        self.assertIn(
            "GATEWAY_HOST_BIND: ${GATEWAY_HOST_BIND:-127.0.0.1}",
            compose_service_block("idp"),
        )
        self.assertIn(
            '"127.0.0.1:${GATEWAY_HOST_PORT:-8088}:80"',
            compose_service_block("nginx"),
        )
        self.assertNotIn('"${GATEWAY_HOST_BIND:-127.0.0.1}:', compose_service_block("nginx"))

    def test_default_compose_delegates_to_the_source_built_stack(self) -> None:
        # docker-compose.yml is now intentionally a compatibility wrapper. The
        # actual gateway template and secret interpolation live in the local
        # source-built stack it includes.
        self.assertIn("include:", LEGACY_COMPOSE)
        self.assertIn("path: ./docker-compose.local.yml", LEGACY_COMPOSE)
        self.assertNotIn("jacksonbarreto/", LEGACY_COMPOSE)


if __name__ == "__main__":
    unittest.main()
