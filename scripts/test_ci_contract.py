from pathlib import Path
import json
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text(
    encoding="utf-8"
)


def job_block(job_id: str) -> str:
    marker = f"  {job_id}:\n"
    start = WORKFLOW.index(marker)
    remaining = WORKFLOW[start + len(marker) :]
    next_job_offsets = [
        offset
        for offset, line in enumerate(remaining.splitlines(keepends=True))
        if line.startswith("  ")
        and not line.startswith("    ")
        and line.rstrip().endswith(":")
    ]
    if not next_job_offsets:
        return WORKFLOW[start:]
    lines = remaining.splitlines(keepends=True)
    return WORKFLOW[start : start + len(marker) + sum(
        len(line) for line in lines[: next_job_offsets[0]]
    )]


class CIWorkflowContractTest(unittest.TestCase):
    def test_release_process_does_not_advertise_a_colliding_same_host_canary(self) -> None:
        release_process = (ROOT / "docs" / "release-process.md").read_text(
            encoding="utf-8"
        )

        self.assertNotIn("## Canary (single-host)", release_process)
        self.assertIn("isolated Docker context or host", release_process)
        self.assertNotIn("scripts/Stop-HAI.ps1", release_process)
        self.assertNotIn("scripts/Start-HAI.ps1", release_process)
        self.assertIn("docker compose --env-file", release_process)

    def test_canonical_service_runtime_images_do_not_float_on_latest(
        self,
    ) -> None:
        backend = (ROOT / "backend" / "Dockerfile").read_text(encoding="utf-8")
        self.assertNotRegex(backend, r"(?m)^FROM\s+\S+:latest(?:\s|$)")
        self.assertRegex(
            backend,
            r"(?m)^FROM alpine:3\.22@sha256:[0-9a-f]{64}$",
        )

        idp = (ROOT / "idp" / "Dockerfile").read_text(encoding="utf-8")
        self.assertNotRegex(idp, r"(?m)^FROM\s+\S+:latest(?:\s|$)")
        self.assertRegex(
            idp,
            r"(?m)^FROM alpine:3\.22@sha256:[0-9a-f]{64}$",
        )

        manager = (ROOT / "nginx-config-manager" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        self.assertNotRegex(manager, r"(?m)^FROM\s+\S+:latest(?:\s|$)")
        self.assertRegex(
            manager,
            r"(?m)^FROM alpine:3\.22@sha256:[0-9a-f]{64}$",
        )

    def test_backend_runtime_is_static_non_root_and_resource_bounded(self) -> None:
        dockerfile = (ROOT / "backend" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        compose = (ROOT / "docker-compose.local.yml").read_text(
            encoding="utf-8"
        )
        backend_start = compose.index("  backend:\n")
        backend_end = compose.index("\n  frontend:\n", backend_start)
        backend = compose[backend_start:backend_end]

        for required in (
            "CGO_ENABLED=0",
            "-trimpath",
            '-ldflags "-s -w"',
            "/var/lib/hai/phase2-state",
            "chown -R 10001:10001",
            "USER 10001:10001",
            'ENTRYPOINT ["/app/hai-backend"]',
        ):
            with self.subTest(required=required):
                self.assertIn(required, dockerfile)
        self.assertNotIn("apt-get", dockerfile)
        self.assertNotIn("curl", dockerfile)

        for required in (
            'user: "10001:10001"',
            "init: true",
            "read_only: true",
            "stop_grace_period: 20s",
            "/tmp:rw,noexec,nosuid,nodev,size=${BACKEND_TMPFS_SIZE:-128m}",
            "mem_limit: ${BACKEND_MEMORY_LIMIT:-512m}",
            "mem_reservation: ${BACKEND_MEMORY_RESERVATION:-96m}",
            "cpus: ${BACKEND_CPU_LIMIT:-1.5}",
            "pids_limit: ${BACKEND_PIDS_LIMIT:-256}",
            "no-new-privileges:true",
            "cap_drop:",
            "- ALL",
            "HAI_PHASE2_STATE_DIR: /var/lib/hai/phase2-state",
            "phase2-control-state:/var/lib/hai/phase2-state",
            "wget -q -O /dev/null",
        ):
            with self.subTest(required=required):
                self.assertIn(required, backend)

    def test_idp_runtime_is_static_non_root_and_resource_bounded(self) -> None:
        dockerfile = (ROOT / "idp" / "Dockerfile").read_text(encoding="utf-8")
        compose = (ROOT / "docker-compose.local.yml").read_text(
            encoding="utf-8"
        )
        idp_start = compose.index("  idp:\n")
        idp_end = compose.index("\n  backend-migrate:\n", idp_start)
        idp = compose[idp_start:idp_end]
        backend_start = idp_end + 1
        backend_end = compose.index("\n  frontend:\n", backend_start)
        backend = compose[backend_start:backend_end]
        nginx_start = compose.index("  nginx:\n")
        nginx_end = compose.index("\n  ngrok:\n", nginx_start)
        nginx = compose[nginx_start:nginx_end]

        for required in (
            "CGO_ENABLED=0",
            "-trimpath",
            '-ldflags "-s -w"',
            "USER 10001:10001",
            'ENTRYPOINT ["/app/idp"]',
        ):
            with self.subTest(required=required):
                self.assertIn(required, dockerfile)
        self.assertNotIn("apt-get", dockerfile)
        self.assertNotIn("curl", dockerfile)
        self.assertNotIn("bash", dockerfile)
        self.assertTrue((ROOT / "idp" / ".dockerignore").is_file())

        for required in (
            'user: "10001:10001"',
            "init: true",
            "read_only: true",
            "stop_grace_period: 20s",
            "/tmp:rw,noexec,nosuid,nodev,size=${IDP_TMPFS_SIZE:-32m}",
            "mem_limit: ${IDP_MEMORY_LIMIT:-256m}",
            "mem_reservation: ${IDP_MEMORY_RESERVATION:-48m}",
            "cpus: ${IDP_CPU_LIMIT:-1.0}",
            "pids_limit: ${IDP_PIDS_LIMIT:-128}",
            "no-new-privileges:true",
            "cap_drop:",
            "- ALL",
            "wget -q -O /dev/null http://127.0.0.1:${WEB_SERVER_PORT}/healthz",
        ):
            with self.subTest(required=required):
                self.assertIn(required, idp)

        self.assertRegex(
            backend,
            r"(?s)idp:\s+condition: service_healthy",
        )
        self.assertRegex(
            nginx,
            r"(?s)backend:\s+condition: service_healthy.*idp:\s+condition: service_healthy",
        )

    def test_frontend_runtime_is_non_root_single_worker_and_resource_bounded(
        self,
    ) -> None:
        dockerfile = (ROOT / "frontend" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        main_config = (ROOT / "frontend" / "nginx-main.conf").read_text(
            encoding="utf-8"
        )
        site_config = (ROOT / "frontend" / "nginx-custom.conf").read_text(
            encoding="utf-8"
        )
        compose = (ROOT / "docker-compose.local.yml").read_text(
            encoding="utf-8"
        )
        frontend_start = compose.index("  frontend:\n")
        frontend_end = compose.index("\n  browser-verifier:\n", frontend_start)
        frontend = compose[frontend_start:frontend_end]

        self.assertRegex(
            dockerfile,
            r"(?m)^FROM node:22\.22\.3-alpine@sha256:[0-9a-f]{64} AS build$",
        )
        self.assertRegex(
            dockerfile,
            r"(?m)^FROM nginx:stable-alpine-slim@sha256:[0-9a-f]{64}$",
        )
        self.assertIn("USER 101:101", dockerfile)
        self.assertIn('ENTRYPOINT ["nginx", "-g", "daemon off;"]', dockerfile)
        self.assertIn("worker_processes 1;", main_config)
        self.assertIn("pid /tmp/nginx.pid;", main_config)
        self.assertIn("client_body_temp_path /tmp/client_temp;", main_config)
        self.assertIn("listen 8080;", site_config)
        self.assertIn("location = /healthz", site_config)

        for required in (
            'user: "101:101"',
            "init: true",
            "read_only: true",
            "/tmp:rw,noexec,nosuid,nodev,size=${FRONTEND_TMPFS_SIZE:-16m}",
            "mem_limit: ${FRONTEND_MEMORY_LIMIT:-64m}",
            "mem_reservation: ${FRONTEND_MEMORY_RESERVATION:-8m}",
            "cpus: ${FRONTEND_CPU_LIMIT:-0.25}",
            "pids_limit: ${FRONTEND_PIDS_LIMIT:-32}",
            "no-new-privileges:true",
            "cap_drop:",
            "- ALL",
            "condition: service_healthy",
            "http://127.0.0.1:8080/healthz",
        ):
            with self.subTest(required=required):
                self.assertIn(required, frontend)

        gateway_template = ROOT / "nginx-config" / "nginx.conf.template"
        self.assertIn(
            "set $frontend_upstream frontend:8080;",
            gateway_template.read_text(encoding="utf-8"),
        )

    def test_local_kafka_protocol_runtime_uses_bounded_single_node_redpanda(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        environment = (ROOT / ".env.example").read_text(encoding="utf-8")
        kafka_start = compose.index("  kafka:\n")
        kafka_end = compose.index("\n  generic-auto:\n", kafka_start)
        kafka = compose[kafka_start:kafka_end]

        for required in (
            "redpandadata/redpanda:v26.2.1@sha256:",
            "internal://0.0.0.0:9092",
            "internal://kafka:9092",
            "--smp",
            '"1"',
            "--overprovisioned=true",
            "--unsafe-bypass-fsync=false",
            "--lock-memory=false",
            "--reserve-memory",
            "--check=false",
            "mem_limit: ${KAFKA_MEMORY_LIMIT:-256m}",
            "mem_reservation: ${KAFKA_MEMORY_RESERVATION:-64m}",
            "cpus: ${KAFKA_CPU_LIMIT:-0.5}",
            "pids_limit: ${KAFKA_PIDS_LIMIT:-96}",
            "no-new-privileges:true",
            "cap_drop:",
            'test: ["CMD", "rpk", "cluster", "info", "-X", "brokers=127.0.0.1:9092"]',
            "redpanda-data:/var/lib/redpanda/data",
        ):
            with self.subTest(required=required):
                self.assertIn(required, kafka)

        self.assertNotIn("cp-kafka", kafka)
        self.assertNotIn("KAFKA_HEAP_OPTS", kafka)
        self.assertNotIn("dev-container", kafka)
        self.assertNotIn("--unsafe-bypass-fsync=true", kafka)
        self.assertNotIn("zookeeper", compose.lower())
        self.assertNotIn("kafka-network", compose)
        self.assertIn("KAFKA_MEMORY_LIMIT=256m", environment)
        self.assertIn("KAFKA_MEMORY_RESERVATION=64m", environment)
        self.assertIn("KAFKA_PIDS_LIMIT=96", environment)

    def test_kafka_acceptance_captures_output_before_matching_under_pipefail(self) -> None:
        workflow = (ROOT / ".github" / "workflows" / "ci.yml").read_text(
            encoding="utf-8"
        )

        self.assertIn('topic_description="$(docker compose', workflow)
        self.assertIn('grep -q PARTITION <<<"$topic_description"', workflow)
        self.assertIn('kafka_command="$(docker compose', workflow)
        self.assertIn("cat /proc/1/cmdline", workflow)
        self.assertNotIn("cluster config get write_caching_default", workflow)
        self.assertIn('kafka_logs="$(docker compose', workflow)
        self.assertNotIn(
            "rpk topic describe hai-ci-contract -X brokers=127.0.0.1:9092 | grep -q",
            workflow,
        )
        self.assertNotIn("logs kafka \\\n            | grep -q", workflow)

    def test_local_persistence_services_are_durable_pinned_and_bounded(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        idp_start = compose.index("\n  postgres-idp:\n") + 1
        automation_start = compose.index("\n  postgres-automation:\n", idp_start) + 1
        redis_start = compose.index("\n  redis:\n", automation_start) + 1
        kafka_start = compose.index("\n  kafka:\n", redis_start) + 1
        idp = compose[idp_start:automation_start]
        automation = compose[automation_start:redis_start]
        redis = compose[redis_start:kafka_start]

        for block, requirements in (
            (
                idp,
                (
                    "postgres:17-alpine@sha256:",
                    "mem_limit: ${POSTGRES_IDP_MEMORY_LIMIT:-128m}",
                    "pids_limit: ${POSTGRES_IDP_PIDS_LIMIT:-64}",
                    "no-new-privileges:true",
                    "shared_buffers=16MB",
                    "max_connections=16",
                    "postgres-idp-data:/var/lib/postgresql/data",
                ),
            ),
            (
                automation,
                (
                    "pgvector:0.8.5-pg17-bookworm@sha256:",
                    "mem_limit: ${POSTGRES_AUTOMATION_MEMORY_LIMIT:-256m}",
                    "pids_limit: ${POSTGRES_AUTOMATION_PIDS_LIMIT:-96}",
                    "no-new-privileges:true",
                    "shared_buffers=32MB",
                    "max_connections=24",
                    "postgres-automation-data:/var/lib/postgresql/data",
                ),
            ),
            (
                redis,
                (
                    "redis:7-alpine@sha256:",
                    "mem_limit: ${REDIS_MEMORY_LIMIT:-128m}",
                    "pids_limit: ${REDIS_PIDS_LIMIT:-32}",
                    "no-new-privileges:true",
                    "--appendonly",
                    "--maxmemory-policy",
                    "noeviction",
                    "redis-data:/data",
                ),
            ),
        ):
            for required in requirements:
                with self.subTest(required=required):
                    self.assertIn(required, block)

    def test_every_local_service_has_bounded_rotating_logs(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        environment = (ROOT / ".env.example").read_text(encoding="utf-8")
        services = compose.split("\nservices:\n", 1)[1].split("\nnetworks:\n", 1)[0]
        service_names = re.findall(r"^  ([a-zA-Z0-9][a-zA-Z0-9_-]*):\n", services, re.MULTILINE)

        self.assertGreater(len(service_names), 30)
        self.assertEqual(
            services.count("    <<: *hai-service-defaults"),
            len(service_names),
        )
        for required in (
            "x-hai-service-defaults: &hai-service-defaults",
            "driver: local",
            "max-size: ${HAI_LOG_MAX_SIZE:-10m}",
            "max-file: ${HAI_LOG_MAX_FILES:-3}",
            'compress: "true"',
        ):
            with self.subTest(required=required):
                self.assertIn(required, compose)
        self.assertIn("HAI_LOG_MAX_SIZE=10m", environment)
        self.assertIn("HAI_LOG_MAX_FILES=3", environment)

    def test_local_health_probes_are_low_churn_and_broker_is_quiet(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")

        self.assertNotIn("interval: 10s", compose)
        self.assertEqual(compose.count("start_interval: 2s"), 9)
        for required in (
            "GIN_MODE: release",
            "--default-log-level",
            "warn",
        ):
            with self.subTest(required=required):
                self.assertIn(required, compose)
        for removed_java_setting in (
            "KAFKA_LOG4J_ROOT_LOGLEVEL",
            "KAFKA_HEAP_OPTS",
            "KAFKA_GC_LOG_OPTS",
        ):
            with self.subTest(removed=removed_java_setting):
                self.assertNotIn(removed_java_setting, compose)

    def test_application_database_pools_are_explicitly_bounded(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        idp = compose[
            compose.index("  idp:\n") : compose.index("\n  backend-migrate:\n")
        ]
        backend = compose[
            compose.index("  backend:\n") : compose.index("\n  frontend:\n")
        ]

        for block, requirements in (
            (
                idp,
                (
                    "DB_MAX_OPEN_CONNS: ${IDP_DB_MAX_OPEN_CONNS:-8}",
                    "DB_MAX_IDLE_CONNS: ${IDP_DB_MAX_IDLE_CONNS:-2}",
                    "DB_CONN_MAX_IDLE_TIME: ${IDP_DB_CONN_MAX_IDLE_TIME:-5m}",
                    "DB_CONN_MAX_LIFETIME: ${IDP_DB_CONN_MAX_LIFETIME:-30m}",
                ),
            ),
            (
                backend,
                (
                    "DB_MAX_OPEN_CONNS: ${BACKEND_DB_MAX_OPEN_CONNS:-16}",
                    "DB_MAX_IDLE_CONNS: ${BACKEND_DB_MAX_IDLE_CONNS:-4}",
                    "DB_CONN_MAX_IDLE_TIME: ${BACKEND_DB_CONN_MAX_IDLE_TIME:-5m}",
                    "DB_CONN_MAX_LIFETIME: ${BACKEND_DB_CONN_MAX_LIFETIME:-30m}",
                ),
            ),
        ):
            for required in requirements:
                with self.subTest(required=required):
                    self.assertIn(required, block)

    def test_generic_compatibility_runtime_is_static_non_root_and_bounded(self) -> None:
        dockerfile = (ROOT / "generic-auto" / "dockerfile").read_text(
            encoding="utf-8"
        )
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        generic_start = compose.index("  generic-auto:\n")
        generic_end = compose.index("\nnetworks:\n", generic_start)
        generic = compose[generic_start:generic_end]

        for required in (
            "golang:1.25.13-alpine@sha256:",
            "CGO_ENABLED=0",
            "FROM scratch",
            "USER 65532:65532",
            'ENTRYPOINT ["/server"]',
        ):
            with self.subTest(required=required):
                self.assertIn(required, dockerfile)
        for required in (
            'profiles: ["compatibility"]',
            'user: "65532:65532"',
            "init: true",
            "read_only: true",
            "mem_limit: ${GENERIC_AUTO_MEMORY_LIMIT:-32m}",
            "pids_limit: ${GENERIC_AUTO_PIDS_LIMIT:-16}",
            "no-new-privileges:true",
            "cap_drop:",
            '- ALL',
            'test: ["CMD", "/server", "-healthcheck"]',
        ):
            with self.subTest(required=required):
                self.assertIn(required, generic)

        gateway_template = (ROOT / "nginx-config" / "nginx.conf.template").read_text(
            encoding="utf-8"
        )
        self.assertNotIn("generic_auto_upstream", gateway_template)
        self.assertIn("location ^~ /generic-auto/", gateway_template)
        self.assertIn("return 404;", gateway_template)
        self.assertFalse(
            (ROOT / "nginx-config" / "sites-enabled" / "generic-auto.conf").exists()
        )

    def test_directly_invoked_contract_and_smoke_files_exist(self) -> None:
        for relative_path in (
            "nginx-config/test_gateway_contract.py",
            "scripts/test_ci_contract.py",
            "scripts/test_smoke_auth_contract.py",
            "scripts/smoke-all.sh",
            "scripts/two-account-isolation-test.sh",
        ):
            with self.subTest(path=relative_path):
                self.assertTrue((ROOT / relative_path).is_file())

    def test_execution_boundary_race_tests_are_not_served_from_test_cache(
        self,
    ) -> None:
        backend = job_block("backend")
        self.assertIn(
            "go test -count=1 -race ./internal/automation ./internal/task",
            backend,
        )

    def test_backend_vulnerability_scan_is_pinned_and_blocking(self) -> None:
        backend = job_block("backend")
        self.assertIn(
            "go install golang.org/x/vuln/cmd/govulncheck@v1.6.0",
            backend,
        )
        self.assertIn("govulncheck ./...", backend)
        self.assertNotIn("continue-on-error", backend)

    def test_idp_vulnerability_scan_is_pinned_and_blocking(self) -> None:
        idp = job_block("idp")
        self.assertIn(
            "go install golang.org/x/vuln/cmd/govulncheck@v1.6.0",
            idp,
        )
        self.assertIn("govulncheck ./...", idp)
        self.assertNotIn("continue-on-error", idp)

    def test_nginx_manager_toolchain_and_scan_match_container(self) -> None:
        go_mod = (ROOT / "nginx-config-manager" / "go.mod").read_text(
            encoding="utf-8"
        )
        dockerfile = (ROOT / "nginx-config-manager" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        job = job_block("nginx-config-manager")

        recommended = re.search(
            r"^toolchain\s+go(\d+\.\d+\.\d+)$",
            go_mod,
            re.MULTILINE,
        )
        container = re.search(
            r"^FROM\s+golang:(\d+\.\d+\.\d+)(?:@sha256:[0-9a-f]{64})?\s+AS\s+builder$",
            dockerfile,
            re.MULTILINE,
        )
        self.assertIsNotNone(recommended)
        self.assertIsNotNone(container)
        self.assertEqual(recommended.group(1), container.group(1))
        self.assertIn(f'go-version: "{recommended.group(1)}"', job)
        self.assertIn("- run: go vet ./...", job)
        self.assertIn(
            "go install golang.org/x/vuln/cmd/govulncheck@v1.6.0",
            job,
        )
        self.assertIn("govulncheck ./...", job)
        self.assertNotIn("continue-on-error", job)

    def test_nginx_manager_has_no_docker_socket_control_path(self) -> None:
        go_mod = (ROOT / "nginx-config-manager" / "go.mod").read_text(
            encoding="utf-8"
        )
        service = (
            ROOT
            / "nginx-config-manager"
            / "internal"
            / "app"
            / "autoconfig"
            / "auto_config_service.go"
        ).read_text(encoding="utf-8")
        self.assertNotIn("github.com/docker/docker", go_mod)
        self.assertNotIn("github.com/docker/docker", service)
        self.assertIn("Docker socket control is disabled", service)
        for compose_file in ("docker-compose.local.yml", "docker-compose.yml"):
            with self.subTest(compose_file=compose_file):
                content = (ROOT / compose_file).read_text(encoding="utf-8")
                self.assertNotIn("/var/run/docker.sock", content)

    def test_compose_entrypoints_delegate_to_one_source_built_topology(self) -> None:
        canonical = (ROOT / "docker-compose.local.yml").read_text(
            encoding="utf-8"
        )

        for entrypoint_path, compose_path, env_path in (
            (ROOT / "docker-compose.yml", "./docker-compose.local.yml", ".env.local"),
            (
                ROOT / "backend" / "docker-compose.yml",
                "../docker-compose.local.yml",
                "../.env.local",
            ),
            (
                ROOT / "idp" / "docker-compose.yml",
                "../docker-compose.local.yml",
                "../.env.local",
            ),
            (
                ROOT / "gate" / "docker-compose.yml",
                "../docker-compose.local.yml",
                "../.env.local",
            ),
            (
                ROOT / "kafka" / "docker-compose.yaml",
                "../docker-compose.local.yml",
                "../.env.local",
            ),
        ):
            entrypoint = entrypoint_path.read_text(encoding="utf-8")
            with self.subTest(entrypoint=entrypoint_path):
                self.assertIn("include:", entrypoint)
                self.assertIn("name: 018-hai", entrypoint)
                self.assertIn(f"path: {compose_path}", entrypoint)
                self.assertIn("HAI_ENV_FILE", entrypoint)
                self.assertIn(env_path, entrypoint)
                self.assertNotIn("jacksonbarreto/", entrypoint)
                self.assertNotIn(":latest", entrypoint)
                self.assertNotIn("kafka2", entrypoint)
                self.assertNotIn("zookeeper", entrypoint)

        development = (ROOT / "docker-compose.dev.yml").read_text(
            encoding="utf-8"
        )
        self.assertNotIn("ports:", development)
        self.assertNotIn("volumes:", development)
        self.assertNotIn("adminer", development)
        self.assertNotIn(":latest", development)
        self.assertNotIn("automation_hub_network", development)
        self.assertNotIn("idp_network", development)

        for service_path in ("./backend", "./frontend", "./idp"):
            with self.subTest(source_build=service_path):
                self.assertIn(f"context: {service_path}", canonical)
        self.assertIn("container_name: 018-hai-backend", canonical)
        self.assertIn("container_name: 018-hai-frontend", canonical)
        self.assertIn("container_name: 018-hai-idp", canonical)
        self.assertNotIn("jacksonbarreto/", canonical)
        self.assertNotIn("kafka2:", canonical)
        self.assertNotIn("kafka3:", canonical)
        self.assertNotIn("zookeeper:", canonical)

    def test_nginx_manager_is_observable_non_root_and_resource_bounded(
        self,
    ) -> None:
        dockerfile = (ROOT / "nginx-config-manager" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        dockerignore = ROOT / "nginx-config-manager" / ".dockerignore"
        compose = (ROOT / "docker-compose.local.yml").read_text(
            encoding="utf-8"
        )
        manager_start = compose.index("  nginxconfigmanager:\n")
        manager_end = compose.index("\n  ortools-solver:\n", manager_start)
        manager = compose[manager_start:manager_end]
        consumer = (
            ROOT
            / "nginx-config-manager"
            / "internal"
            / "app"
            / "autoconfig"
            / "consumer.go"
        ).read_text(encoding="utf-8")
        config_writer = (
            ROOT
            / "nginx-config-manager"
            / "internal"
            / "app"
            / "autoconfig"
            / "auto_config_service.go"
        ).read_text(encoding="utf-8")
        inbox = (
            ROOT
            / "nginx-config-manager"
            / "internal"
            / "app"
            / "autoconfig"
            / "inbox.go"
        ).read_text(encoding="utf-8")

        for required in (
            "CGO_ENABLED=0",
            "-trimpath",
            '-ldflags "-s -w"',
            "USER 10001:10001",
            'ENTRYPOINT ["/app/nginx-config-manager"]',
        ):
            with self.subTest(required=required):
                self.assertIn(required, dockerfile)
        self.assertNotIn("apt-get", dockerfile)
        self.assertTrue(dockerignore.is_file())

        for required in (
            'user: "10001:10001"',
            "init: true",
            "read_only: true",
            "/tmp:rw,noexec,nosuid,nodev,size=${NGINX_CONFIG_MANAGER_TMPFS_SIZE:-16m}",
            "mem_limit: ${NGINX_CONFIG_MANAGER_MEMORY_LIMIT:-128m}",
            "mem_reservation: ${NGINX_CONFIG_MANAGER_MEMORY_RESERVATION:-16m}",
            "cpus: ${NGINX_CONFIG_MANAGER_CPU_LIMIT:-0.5}",
            "pids_limit: ${NGINX_CONFIG_MANAGER_PIDS_LIMIT:-64}",
            "no-new-privileges:true",
            "cap_drop:",
            "- ALL",
            "condition: service_healthy",
            "/healthz",
            "NGINX_CONFIG_MANAGER_GROUP_ID:",
            "NGINX_CONFIG_MANAGER_INBOX_DIR:",
        ):
            with self.subTest(required=required):
                self.assertIn(required, manager)

        self.assertIn("sarama.NewConsumerGroup", consumer)
        self.assertIn("Consumer.Offsets.Initial = sarama.OffsetOldest", consumer)
        self.assertIn("h.inbox.Process", consumer)
        self.assertIn("session.MarkMessage", consumer)
        self.assertIn("ready.Store(true)", consumer)
        self.assertNotIn("ConsumePartition", consumer)
        self.assertNotIn("sarama.OffsetNewest", consumer)
        self.assertIn("os.O_EXCL", inbox)
        self.assertIn('".dead.json"', inbox)
        self.assertIn("maxAttempts", inbox)
        self.assertIn("Prune", inbox)
        self.assertIn("os.CreateTemp", config_writer)
        self.assertIn("file.Sync()", config_writer)
        self.assertIn("os.Rename", config_writer)

    def test_frontend_toolchain_and_security_gate_are_pinned(self) -> None:
        package = (ROOT / "frontend" / "package.json").read_text(
            encoding="utf-8"
        )
        main = (ROOT / "frontend" / "src" / "main.ts").read_text(
            encoding="utf-8"
        )
        angular_path = ROOT / "frontend" / "angular.json"
        angular = angular_path.read_text(encoding="utf-8")
        angular_config = json.loads(angular)
        dockerfile = (ROOT / "frontend" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        frontend = job_block("frontend")

        self.assertIn('"packageManager": "npm@10.9.8"', package)
        self.assertIn('"@angular/core": "22.1.1"', package)
        self.assertIn('"@angular/build": "22.1.3"', package)
        self.assertIn('"builder": "@angular/build:application"', angular)
        production_optimization = angular_config["projects"]["app"]["architect"][
            "build"
        ]["configurations"]["production"]["optimization"]
        self.assertTrue(production_optimization["scripts"])
        self.assertTrue(production_optimization["styles"]["minify"])
        self.assertFalse(production_optimization["styles"]["inlineCritical"])
        self.assertTrue(production_optimization["fonts"])
        self.assertIn("provideZoneChangeDetection", main)
        self.assertIn("applicationProviders:", main)
        self.assertIn("eventCoalescing: true", main)
        self.assertNotIn("runCoalescing: true", main)
        http_refresh = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "services"
            / "http-view-refresh.interceptor.ts"
        ).read_text(encoding="utf-8")
        self.assertIn("HttpViewRefreshScheduler", http_refresh)
        self.assertIn("HttpEventType.Response", http_refresh)
        component_paths = sorted(
            (ROOT / "frontend" / "src" / "app").rglob("*.component.ts")
        )
        self.assertGreater(len(component_paths), 0)
        for component_path in component_paths:
            with self.subTest(component_path=component_path):
                component = component_path.read_text(encoding="utf-8")
                if component_path.name == "app.component.ts" or component_path.name == "app-shell.component.ts":
                    self.assertNotIn("ChangeDetectionStrategy.OnPush", component)
                    continue
                self.assertRegex(
                    component,
                    r"changeDetection:\s*ChangeDetectionStrategy\.(?:Eager|OnPush)",
                )
        self.assertRegex(
            dockerfile,
            r"(?m)^FROM node:22\.22\.3-alpine@sha256:[0-9a-f]{64} AS build$",
        )
        self.assertIn('node-version: "22.22.3"', frontend)
        self.assertIn("npm ci --no-audit --no-fund", frontend)
        self.assertIn("npm audit --audit-level=high", frontend)
        self.assertNotIn("continue-on-error", frontend)
        self.assertFalse((ROOT / "frontend" / "pnpm-lock.yaml").exists())
        self.assertFalse((ROOT / "frontend" / "pnpm-workspace.yaml").exists())

    def test_ngrok_profile_is_opt_in_pinned_and_preflight_gated(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        workflow = (ROOT / ".github" / "workflows" / "ci.yml").read_text(
            encoding="utf-8"
        )
        preflight = (ROOT / "scripts" / "start-ngrok.ps1").read_text(
            encoding="utf-8"
        )
        generator = (ROOT / "scripts" / "prepare-ngrok-windows.ps1").read_text(
            encoding="utf-8"
        )
        discovery = (ROOT / "scripts" / "discover-ngrok-windows.ps1").read_text(
            encoding="utf-8"
        )
        a2a_smoke = (ROOT / "scripts" / "smoke-a2a-bridge.ps1").read_text(
            encoding="utf-8"
        )
        ownership_test = (
            ROOT / "scripts" / "test-ngrok-ownership.ps1"
        ).read_text(encoding="utf-8")
        config = (ROOT / "deploy" / "ngrok" / "ngrok.yml").read_text(
            encoding="utf-8"
        )
        entrypoint = (ROOT / "deploy" / "ngrok" / "start-ngrok.sh").read_text(
            encoding="utf-8"
        )
        public_policy = (ROOT / "deploy" / "ngrok" / "public-policy.yml").read_text(
            encoding="utf-8"
        )
        private_policy = (
            ROOT / "deploy" / "ngrok" / "private-a2a-policy.yml"
        ).read_text(encoding="utf-8")
        ngrok_start = compose.index("  ngrok:\n")
        ngrok_end = compose.index("\n  nginxconfigmanager:\n", ngrok_start)
        ngrok_service = compose[ngrok_start:ngrok_end]

        self.assertIn('profiles: ["cloud-tunnel"]', ngrok_service)
        self.assertRegex(
            ngrok_service,
            r"image: ngrok/ngrok:alpine@sha256:[0-9a-f]{64}",
        )
        self.assertNotIn("\n    ports:", ngrok_service)
        self.assertIn("read_only: true", ngrok_service)
        self.assertIn("no-new-privileges:true", ngrok_service)
        self.assertIn("cap_drop:", ngrok_service)
        self.assertIn('entrypoint: ["/bin/sh", "/etc/hai/start-ngrok.sh"]', ngrok_service)
        self.assertIn(
            "./deploy/ngrok/public-policy.yml:/etc/hai/public-policy.yml:ro",
            ngrok_service,
        )
        self.assertIn(
            "./deploy/ngrok/private-a2a-policy.yml:/etc/hai/private-a2a-policy.yml:ro",
            ngrok_service,
        )
        for required in (
            "LOCAL_LOGIN_BYPASS_ENABLED",
            "IDP_COOKIE_SECURE",
            "GATEWAY_HOST_BIND",
            "RATE_LIMIT_PER_MINUTE",
            "NGROK_AUTHTOKEN",
            "HAI_NGROK_URL",
            "GOOGLE_LOGIN_REDIRECT_URL",
            "GOOGLE_OAUTH_REDIRECT_URL",
            "docker compose",
        ):
            with self.subTest(required=required):
                self.assertIn(required, preflight)
        for required in (
            'LOCAL_LOGIN_BYPASS_ENABLED = "false"',
            'IDP_COOKIE_SECURE = "true"',
            'GATEWAY_HOST_BIND = "127.0.0.1"',
            'RATE_LIMIT_PER_MINUTE = $RateLimitPerMinute.ToString()',
            'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED',
            'HAI_A2A_BRIDGE_URL" "$origin/api/v1/a2a"',
            'start-ngrok.ps1") -ValidateOnly',
            'Remove-Item -LiteralPath $outputPath',
        ):
            with self.subTest(cloud_profile_generator=required):
                self.assertIn(required, generator)
        for required in (
            "Get-NetTCPConnection",
            "Start-Process",
            "-WindowStyle Hidden",
            "127.0.0.1:$InspectionPort/api/tunnels",
            "Stop-Process -Id $process.Id",
            "Test-NgrokHost",
        ):
            with self.subTest(endpoint_discovery=required):
                self.assertIn(required, discovery)
        self.assertNotIn("--pooling-enabled", discovery)
        secured_up = "up -d --no-build idp backend frontend nginx"
        tunnel_up = "up -d --no-build ngrok"
        self.assertIn(secured_up, preflight)
        self.assertIn(tunnel_up, preflight)
        self.assertLess(preflight.index(secured_up), preflight.index(tunnel_up))
        self.assertIn("remote_management: false", config)
        self.assertIn("update_check: false", config)
        self.assertIn("inspect_db_size: -1", config)
        self.assertIn("http://nginx:8080", entrypoint)
        self.assertIn("Test-NgrokHostname", preflight)
        self.assertIn("HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED", preflight)
        self.assertIn("HAI_A2A_BRIDGE_TOKEN", preflight)
        self.assertIn("/api/v1/a2a", preflight)
        self.assertIn("Test-PublicOrigin $publicUrlText $publicA2A", preflight)
        self.assertIn("Test-PublicEndpointOwnership $publicUrlText $currentProjectTunnelRunning", preflight)
        self.assertIn("HttpCompletionOption]::ResponseHeadersRead", preflight)
        self.assertIn("Read-BoundedResponseBody", preflight)
        self.assertIn("will not attempt to take over another application endpoint", preflight)
        self.assertIn("test-ngrok-ownership.ps1", workflow)
        self.assertIn("non-HAI application", ownership_test)
        self.assertIn("different HAI tunnel", ownership_test)
        self.assertIn("$healthPayload.status -ne 'ok'", preflight)
        self.assertIn("$healthPayload.service -ne 'backend'", preflight)
        self.assertIn('"$BaseUrl/readyz"', preflight)
        self.assertIn("$readinessPayload.PSObject.Properties['checks']", preflight)
        self.assertIn("$readyPayload.PSObject.Properties['checks']", preflight)
        self.assertIn("public readiness response was not marked no-store", preflight)

        self.assertIn("local readiness response was not marked no-store", preflight)
        self.assertIn("smoke-a2a-bridge.ps1", preflight)
        self.assertIn("the tunnel was stopped", preflight)
        self.assertIn("Strict-Transport-Security", preflight)
        self.assertIn("Cache-Control", preflight)
        self.assertIn("no-store", preflight)
        self.assertIn("Content-Security-Policy", preflight)
        self.assertIn("X-Content-Type-Options", preflight)
        self.assertIn("X-Frame-Options", preflight)
        self.assertIn("ngrok-skip-browser-warning", preflight)
        self.assertIn("ngrok-skip-browser-warning", a2a_smoke)
        self.assertIn("if ($Public)", a2a_smoke)
        self.assertIn("--traffic-policy-file=\"$policy_file\"", entrypoint)
        self.assertIn("private-a2a-policy.yml", entrypoint)
        self.assertIn("public-policy.yml", entrypoint)
        for policy in (public_policy, private_policy):
            with self.subTest(policy="hsts"):
                self.assertIn("on_http_response:", policy)
                self.assertIn("type: add-headers", policy)
                self.assertIn("strict-transport-security", policy)
                self.assertIn("max-age=31536000", policy)
        for blocked_path in (
            "/.well-known/agent-card.json",
            "/api/v1/a2a",
        ):
            with self.subTest(blocked_path=blocked_path):
                self.assertIn(blocked_path, private_policy)
        self.assertIn("type: custom-response", private_policy)
        self.assertIn("status_code: 404", private_policy)
        self.assertNotIn("custom-response", public_policy)
        self.assertIn("Ngrok container fail-closed gate", workflow)
        self.assertIn("sh -n deploy/ngrok/start-ngrok.sh", workflow)
        self.assertIn("mismatched public A2A origin unexpectedly passed", workflow)
        for required in (
            "HAI_A2A_BRIDGE_ENABLED",
            "HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED",
            "HAI_A2A_BRIDGE_TOKEN",
            "HAI_A2A_BRIDGE_OWNER_ID",
            "HAI_A2A_BRIDGE_URL",
            "RATE_LIMIT_PER_MINUTE",
        ):
            with self.subTest(ngrok_environment=required):
                self.assertIn(required, ngrok_service)
        for required in (
            'RUN_MODE must be production',
            'local login bypass must be false',
            'secure IDP cookies are required',
            'gateway host bind must remain loopback-only',
            'RATE_LIMIT_PER_MINUTE must be a positive integer',
            'a dedicated ngrok authtoken is required',
            'HAI_NGROK_VALIDATE_ONLY',
            'public A2A requires HAI_A2A_BRIDGE_ENABLED=true',
            'public A2A requires a dedicated 32+ character bridge token',
            'public A2A requires one named owner',
            'public A2A URL must exactly match the fixed ngrok origin',
            '/bin/ngrok http http://nginx:8080',
        ):
            with self.subTest(entrypoint_required=required):
                self.assertIn(required, entrypoint)

    def test_local_ollama_profile_is_private_pinned_and_resource_bounded(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        smoke = (ROOT / "scripts" / "smoke-ollama-provider.ps1").read_text(
            encoding="utf-8"
        )
        policy = (ROOT / "backend" / "internal" / "llm" / "policy.go").read_text(
            encoding="utf-8"
        )
        service_start = compose.index("  ollama-local:\n")
        service_end = compose.index("\n  agent-framework-runner:\n", service_start)
        service = compose[service_start:service_end]

        self.assertIn('profiles: ["local-model"]', service)
        self.assertRegex(
            service,
            r"ollama/ollama:0\.32\.11@sha256:[0-9a-f]{64}",
        )
        self.assertNotIn("\n    ports:", service)
        self.assertIn("networks: [service-hub]", service)
        self.assertIn("read_only: true", service)
        self.assertIn('cap_drop: ["ALL"]', service)
        self.assertIn('OLLAMA_MAX_LOADED_MODELS: "1"', service)
        self.assertIn('OLLAMA_NUM_PARALLEL: "1"', service)
        self.assertIn("HAI_OLLAMA_MEMORY_LIMIT:-2g", service)
        self.assertIn("HAI_OLLAMA_CPU_LIMIT:-2.0", service)
        self.assertIn("ollama-local-data:/root/.ollama", service)
        self.assertIn("LLM_MAX_OUTPUT_TOKENS", compose)
        self.assertIn(
            '"num_predict": boundedGenerationMaxTokens(request.MaxTokens)', policy
        )
        self.assertIn('"max_tokens":  maxTokens', policy)
        for required in (
            "/api/v1/llm/probes",
            "/api/v1/llm/route",
            "/api/v1/llm/generate",
            "/api/v1/llm/generations?limit=10",
            "provider_reported",
            "estimatedCostEur -ne 0",
            "PSObject.Properties.Name -contains 'output'",
            "readiness.summary.warn -ne 0",
        ):
            with self.subTest(local_model_acceptance=required):
                self.assertIn(required, smoke)

    def test_windows_initializer_generates_every_required_production_secret(self) -> None:
        initializer = (ROOT / "scripts" / "initialize-windows.ps1").read_text(
            encoding="utf-8"
        )
        unix_generator = (ROOT / "scripts" / "generate-secrets.sh").read_text(
            encoding="utf-8"
        )
        for required in (
            "BACKEND_API_SHARED_KEY",
            "HAI_MEMORY_ENCRYPTION_KEY",
            "JWT_SECRET",
            "HAI_APPROVAL_PROOF_SIGNING_KEY",
            "DB_PASSWORD",
            "DB_RUNTIME_PASSWORD",
            "HAI_A2A_BRIDGE_TOKEN",
        ):
            with self.subTest(required=required):
                self.assertIn(required, initializer)
                self.assertIn(required, unix_generator)
        self.assertIn("RandomNumberGenerator", initializer)
        self.assertIn('LOCAL_LOGIN_BYPASS_ENABLED\" \"false', initializer)
        self.assertIn('GATEWAY_HOST_BIND\" \"127.0.0.1', initializer)
        self.assertIn('RUN_MODE\" \"production', initializer)
        self.assertIn('HAI_A2A_BRIDGE_ENABLED\" \"true', initializer)
        self.assertIn('HAI_A2A_BRIDGE_OWNER_ID\" $AdminEmail', initializer)
        self.assertIn('HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED\" \"false', initializer)
        self.assertIn('http://127.0.0.1:$GatewayPort/api/v1/a2a', initializer)
        self.assertIn("were not printed", initializer)
        self.assertIn(
            "DB_PASSWORD=change-this-database-owner-password",
            (ROOT / ".env.example").read_text(encoding="utf-8"),
        )

        ngrok_preflight = (ROOT / "scripts" / "start-ngrok.ps1").read_text(
            encoding="utf-8"
        )
        self.assertIn("Require-Secret $settings 'DB_PASSWORD' 32", ngrok_preflight)
        self.assertIn(
            "Require-Secret $settings 'DB_RUNTIME_PASSWORD' 32", ngrok_preflight
        )

    def test_backend_database_owner_is_separated_from_runtime(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        migrator = compose[
            compose.index("  backend-migrate:\n") : compose.index("\n  backend:\n")
        ]
        backend = compose[
            compose.index("  backend:\n") : compose.index("\n  frontend:\n")
        ]
        runtime_role = (
            ROOT / "backend" / "internal" / "infra" / "runtime_role.go"
        ).read_text(encoding="utf-8")

        for required in (
            'command: ["migrate", "up"]',
            "DB_USER: ${DB_USER}",
            "DB_PASSWORD: ${DB_PASSWORD}",
            "DB_RUNTIME_USER:",
            "DB_RUNTIME_PASSWORD:",
            'restart: "no"',
            "read_only: true",
            "no-new-privileges:true",
        ):
            with self.subTest(migrator_required=required):
                self.assertIn(required, migrator)
        for required in (
            "DB_USER: ${DB_RUNTIME_USER:-hai_runtime}",
            "DB_PASSWORD: ${DB_RUNTIME_PASSWORD:-change-this-runtime-database-password}",
            'DB_RUN_MIGRATIONS: "false"',
            "backend-migrate:\n        condition: service_completed_successfully",
        ):
            with self.subTest(backend_required=required):
                self.assertIn(required, backend)
        self.assertNotIn("DB_PASSWORD: ${DB_PASSWORD}", backend)
        for required in (
            "NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS",
            "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES",
            "REVOKE ALL PRIVILEGES ON TABLE public.schema_migrations",
            "REVOKE CREATE ON SCHEMA public FROM PUBLIC",
            "revokeRuntimeRoleMemberships",
            "rejectRuntimeObjectOwnership",
            '"ALTER ROLE " + quotedRole + " RESET ALL"',
        ):
            with self.subTest(role_required=required):
                self.assertIn(required, runtime_role)

    def test_idp_toolchain_matches_ci_and_container(self) -> None:
        go_mod = (ROOT / "idp" / "go.mod").read_text(encoding="utf-8")
        dockerfile = (ROOT / "idp" / "Dockerfile").read_text(encoding="utf-8")
        idp = job_block("idp")

        language = re.search(r"^go\s+(\d+\.\d+)(?:\.\d+)?$", go_mod, re.MULTILINE)
        recommended = re.search(
            r"^toolchain\s+go(\d+\.\d+\.\d+)$",
            go_mod,
            re.MULTILINE,
        )
        container = re.search(
            r"^FROM\s+golang:(\d+\.\d+\.\d+)(?:@sha256:[0-9a-f]{64})?\s+AS\s+builder$",
            dockerfile,
            re.MULTILINE,
        )
        ci = re.search(r'go-version:\s+"(\d+\.\d+\.\d+)"', idp)

        self.assertIsNotNone(language)
        self.assertIsNotNone(recommended)
        self.assertIsNotNone(container)
        self.assertIsNotNone(ci)
        self.assertEqual(recommended.group(1), container.group(1))
        self.assertEqual(recommended.group(1), ci.group(1))
        self.assertEqual(
            ".".join(recommended.group(1).split(".")[:2]),
            language.group(1),
        )
        self.assertIn("- run: go vet ./...", idp)

    def test_backend_toolchain_matches_ci_and_container(self) -> None:
        go_mod = (ROOT / "backend" / "go.mod").read_text(encoding="utf-8")
        dockerfile = (ROOT / "backend" / "Dockerfile").read_text(
            encoding="utf-8"
        )

        language = re.search(r"^go\s+(\d+\.\d+)(?:\.\d+)?$", go_mod, re.MULTILINE)
        recommended = re.search(
            r"^toolchain\s+go(\d+\.\d+\.\d+)$",
            go_mod,
            re.MULTILINE,
        )
        container = re.search(
            r"^FROM\s+golang:(\d+\.\d+\.\d+)(?:@sha256:[0-9a-f]{64})?\s+AS\s+builder$",
            dockerfile,
            re.MULTILINE,
        )

        self.assertIsNotNone(language)
        self.assertIsNotNone(recommended)
        self.assertIsNotNone(container)
        self.assertEqual(recommended.group(1), container.group(1))
        self.assertEqual(
            ".".join(recommended.group(1).split(".")[:2]),
            language.group(1),
        )
        for job_id in (
            "backend",
            "authenticated-smoke",
            "migrations-integration",
            "isolation-acceptance",
        ):
            with self.subTest(job=job_id):
                self.assertIn(
                    f'go-version: "{recommended.group(1)}"',
                    job_block(job_id),
                )

    def test_authenticated_smoke_requires_each_suite_result(self) -> None:
        smoke = job_block("authenticated-smoke")
        self.assertIn(
            'smoke_log="$RUNNER_TEMP/authenticated-smoke.log"',
            smoke,
        )
        for suite in (
            "smoke-background-operations",
            "smoke-model-intelligence",
            "smoke-runtime-lab",
            "smoke-account-bridges",
            "smoke-windows-runtime",
        ):
            with self.subTest(suite=suite):
                self.assertIn(suite, smoke)
        self.assertIn(
            r'grep -Eq "^  PASS  ${suite}  \(Result: [1-9][0-9]* passed, 0 failed\)$"',
            smoke,
        )
        self.assertIn(
            "grep -qx '==> ALL PHASE 2 SMOKE SUITES PASSED'",
            smoke,
        )

    def test_postgres_jobs_cannot_silently_skip_or_match_no_tests(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in (
            "hai_migration_runner_test",
            "hai_framework_registry_test",
            "hai_task_state_test",
            "hai_agent_registry_test",
            "hai_ambient_monitor_test",
            "createdb",
            'HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS: "true"',
            'HAI_REQUIRE_POSTGRES_INTEGRATION: "true"',
            'HAI_TEST_DATABASE_DSN="$migration_dsn" go test -count=1 -tags integration',
            'HAI_TEST_DATABASE_DSN="$registry_dsn" go test -count=1 -tags integration',
            'HAI_TEST_DATABASE_DSN="$task_dsn" go test -count=1 -tags integration',
            'HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN="$ambient_monitor_dsn" go test -p 1 -count=1',
            "^--- PASS: TestRunMigrationsAppliesAndIsIdempotent",
            "^--- PASS: TestRollbackMigrationReversesPostMigration",
            "^--- PASS: TestConcurrentMigrationRunnersSerializeAndRecheck",
            "^--- PASS: TestLegacyBaselineRejectsDifferentExistingPrimaryKey",
            "^--- PASS: TestFrameworkRegistryPostgresIntegrationRequiredEnvironment",
            "^--- PASS: TestFrameworkRegistryPostgresMigrationApplyRollbackAndRerun",
            "^--- PASS: TestFrameworkRegistryPostgresConstraintsAndImmutability",
            "^--- PASS: TestPostgresTaskStateRepositoryDurabilityOwnerScopeAndImmutability",
            "^--- PASS: TestPostgresRepositoryRoundTripOwnerIsolationCASAndImmutableLedgers",
            "^--- PASS: TestPostgresAgentRegistryMigrationCanReplayAgainstExistingSchema",
            "^--- PASS: TestPostgresCollectorsMatchOwnerScopedCanonicalRecords",
            "^--- PASS: TestPostgresCollectorSnapshotUsesReadOnlyRepeatableRead",
            "^--- PASS: TestPostgresRepositoryLifecycle",
            "^--- PASS: TestPostgresCompositionRepositoryLifecycle",
            "^--- PASS: TestPostgresAdvisoryReleaseLifecycle",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, migrations)
        self.assertIn(
            "-run '^TestPostgresTaskStateRepositoryDurabilityOwnerScopeAndImmutability$'",
            migrations,
        )
        database_assignments = dict(
            re.findall(
                r'^\s*(migration|registry|task|agent_registry|ambient_monitor)_dsn="[^"]*dbname=([^ "\n]+)',
                migrations,
                re.MULTILINE,
            )
        )
        self.assertEqual(
            database_assignments,
            {
                "migration": "hai_migration_runner_test",
                "registry": "hai_framework_registry_test",
                "task": "hai_task_state_test",
                "agent_registry": "hai_agent_registry_test",
                "ambient_monitor": "hai_ambient_monitor_test",
            },
        )
        self.assertEqual(len(set(database_assignments.values())), 5)

    def test_running_stack_must_be_live_before_acceptance_test(self) -> None:
        isolation = job_block("isolation-acceptance")
        for contract in (
            'backend_live=""',
            'if ! kill -0 "$(cat backend.pid)" 2>/dev/null; then',
            '[ -n "${backend_live}" ] || {',
            'readyz_http="$(curl -sS -o readyz.json',
            '[ "${readyz_http}" = "200" ] || {',
            'status not in {"ready", "degraded"}',
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, isolation)
        self.assertNotIn(
            "curl -s http://localhost:8080/readyz || true",
            isolation,
        )

    def test_windows_contract_executes_from_a_space_containing_path(self) -> None:
        windows = job_block("windows-contract")
        for contract in (
            "runs-on: windows-latest",
            r'C:\Program Files\Git\bin\bash.exe',
            '"HAI smoke path with spaces"',
            "cygpath -u",
            'python3() { python "$@"; }',
            "hai_smoke_mint_jwt owner ci-secret windows-owner",
            "python scripts/test_ci_contract.py",
            "python scripts/test_smoke_auth_contract.py",
            "scripts/discover-ngrok-windows.ps1",
            r".\scripts\start-ngrok.ps1 -ValidateOnly",
            "scripts/prepare-ngrok-windows.ps1",
            "scripts/backup-windows.ps1",
            "scripts/test-restore-windows.ps1",
            "test-windows-recovery-contract.ps1",
            r".\scripts\initialize-windows.ps1",
            "Generated Windows environment still contains a shipped placeholder",
            "Insecure example environment unexpectedly passed ngrok preflight",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, windows)

    def test_windows_backup_covers_identity_automation_media_and_recovery(self) -> None:
        backup = (ROOT / "scripts" / "backup-windows.ps1").read_text(encoding="utf-8")
        for contract in (
            'Require-Setting $settings "AUTOMATION_DB_NAME"',
            'Require-Setting $settings "IDP_DB_NAME"',
            'pg_restore --list $temporaryFiles[0].Path',
            'pg_restore --list $temporaryFiles[1].Path',
            '[IO.Compression.ZipFile]::CreateFromDirectory',
            'phase2-control-state.tar.gz',
            '$controlStateVolume = "018-hai-phase2-control-state"',
            '${controlStateVolume}:/source:ro',
            'formatVersion = 2',
            'Get-FileHash -Algorithm SHA256',
            'Wait-ContainerHealthy "018-hai-idp"',
            'Wait-ContainerHealthy "018-hai-backend"',
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, backup)

        restore = (ROOT / "scripts" / "test-restore-windows.ps1").read_text(encoding="utf-8")
        recovery_contract = (ROOT / "scripts" / "windows-recovery-contract.ps1").read_text(encoding="utf-8")
        restore_contract = restore + recovery_contract
        for contract in (
            '@("automation.dump", "identity.dump", "media.zip", "phase2-control-state.tar.gz")',
            '$Manifest.controlStateSource -ne "018-hai-phase2-control-state"',
            '018-hai-phase2-restore-drill-',
            'docker volume rm',
            'if ($scratchAutomation -eq $liveAutomation -or $scratchIdentity -eq $liveIdentity)',
            'pg_restore -U $dbUser --exit-on-error --no-owner --no-privileges',
            "automation restore contains no public tables",
            "identity restore contains no public tables",
            'dropdb -U $dbUser --if-exists $scratchAutomation',
            'dropdb -U $dbUser --if-exists $scratchIdentity',
        ):
            with self.subTest(contract=contract):
                self.assertIn(
                    contract,
                    restore_contract.lower() if "restore contains" in contract else restore_contract,
                )

        documentation = (ROOT / "docs" / "backup-restore.md").read_text(encoding="utf-8")
        self.assertIn("Emergency-stop and autonomy controls", documentation)
        self.assertIn("phase2-control-state.tar.gz", documentation)

    def test_smoke_aggregator_rejects_zero_or_missing_assertions(self) -> None:
        aggregator = (ROOT / "scripts" / "smoke-all.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn(
            r"grep -E '^==> Result: [1-9][0-9]* passed, 0 failed$'",
            aggregator,
        )
        self.assertIn(
            'if [ "${code}" -eq 0 ] && [ -n "${valid_line}" ]; then',
            aggregator,
        )
        self.assertIn(
            'line="${reported_line:-==> Result: missing or invalid}"',
            aggregator,
        )

    def test_ci_never_uploads_generated_runtime_or_secret_artifacts(self) -> None:
        self.assertIn("actions/upload-artifact@v4", WORKFLOW)
        self.assertIn("name: hai-windows-installer", WORKFLOW)
        self.assertIn("path: installer/release/HAI-Setup-*.exe", WORKFLOW)
        self.assertNotIn("installer/release/payload", WORKFLOW)
        self.assertNotIn("payload-manifest.json", WORKFLOW)
        self.assertNotRegex(WORKFLOW, r"(?i)\bupload[\w -]*(?:log|env|secret)")

    def test_windows_installer_ci_compiles_the_distributable(self) -> None:
        installer = job_block("windows-installer")
        for contract in (
            "runs-on: windows-latest",
            "choco install innosetup --yes --no-progress",
            "build-windows-installer.ps1 -Version",
            "actions/upload-artifact@v4",
            "retention-days: 14",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, installer)

    def test_every_job_has_an_explicit_timeout(self) -> None:
        for job_id in (
            "windows-installer",
            "backend",
            "idp",
            "nginx-config-manager",
            "frontend",
            "compose",
            "authenticated-smoke",
            "migrations-integration",
            "isolation-acceptance",
            "windows-contract",
        ):
            with self.subTest(job=job_id):
                self.assertIn("timeout-minutes:", job_block(job_id))


if __name__ == "__main__":
    unittest.main()
