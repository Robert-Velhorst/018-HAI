package main

import (
	"os"
	"strings"
	"testing"
)

func TestBackendContainerRuntimeContract(t *testing.T) {
	dockerfile, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("read backend Dockerfile: %v", err)
	}
	docker := strings.ReplaceAll(string(dockerfile), "\r\n", "\n")
	fixtureStart := strings.Index(docker, "FROM builder AS provider-fixture-builder")
	fixtureEnd := strings.Index(docker, "FROM builder AS backend-builder")
	if fixtureStart < 0 || fixtureEnd <= fixtureStart {
		t.Fatal("provider fixture stages are missing or out of order")
	}
	fixtureStages := docker[fixtureStart:fixtureEnd]
	for _, contract := range []string{
		"GOMAXPROCS=\"$HAI_GO_BUILD_WORKERS\" CGO_ENABLED=0 GOOS=linux",
		"go build -p \"$HAI_GO_BUILD_WORKERS\" -o /provider-fixture ./cmd/provider-fixture",
		"FROM scratch AS provider-fixture",
		"COPY --from=provider-fixture-builder /provider-fixture /provider-fixture",
		"USER 65532:65532",
		"ENTRYPOINT [\"/provider-fixture\"]",
	} {
		if !strings.Contains(fixtureStages, contract) {
			t.Errorf("provider fixture contract lost %q", contract)
		}
	}

	finalStageStart := strings.LastIndex(docker, "FROM ubuntu:24.04")
	if finalStageStart < 0 {
		t.Fatal("final Ubuntu runtime stage is missing")
	}
	finalStage := docker[finalStageStart:]
	for _, contract := range []string{
		"install -d -o 10001 -g 10001 /app /root/images /root/agent-workspaces /root/phase2-control-state",
		"WORKDIR /app",
		"COPY --from=backend-builder --chown=10001:10001 /app/cmd/app /app/app",
		"COPY --from=backend-builder --chown=10001:10001 /app/docs /app/docs",
		"USER 10001:10001",
		"CMD [\"/app/app\"]",
	} {
		if !strings.Contains(finalStage, contract) {
			t.Errorf("final backend stage contract lost %q", contract)
		}
	}
	if strings.Contains(finalStage, "USER root") || strings.Contains(finalStage, "CMD [\"/root/app\"]") {
		t.Fatal("final backend process must not run as root or from the root home")
	}
}

func TestBackendComposeWritablePathsAndMigrationContract(t *testing.T) {
	composeBytes, err := os.ReadFile("../../docker-compose.local.yml")
	if err != nil {
		t.Fatalf("read local Compose file: %v", err)
	}
	compose := strings.ReplaceAll(string(composeBytes), "\r\n", "\n")
	backendStart := strings.Index(compose, "\n  backend:\n") + 1
	migrateStart := strings.Index(compose, "\n  backend-migrate:\n") + 1
	permissionsStart := strings.Index(compose, "\n  backend-state-permissions:\n") + 1
	runtimeRoleStart := strings.Index(compose, "\n  backend-runtime-role:\n") + 1
	if backendStart < 1 || migrateStart <= backendStart || permissionsStart <= migrateStart || runtimeRoleStart <= permissionsStart {
		t.Fatal("backend Compose service boundaries are missing or out of order")
	}
	backend := compose[backendStart:migrateStart]
	migration := compose[migrateStart:permissionsStart]
	permissions := compose[permissionsStart:runtimeRoleStart]
	sharedImage := "image: \"${COMPOSE_PROJECT_NAME:-hai}-backend:local\""
	if !strings.Contains(backend, sharedImage) || !strings.Contains(permissions, sharedImage) {
		t.Fatal("backend and state permission migration must use the same built image")
	}
	if !strings.Contains(backend, "      backend-state-permissions:\n        condition: service_completed_successfully\n") {
		t.Fatal("backend must wait for the state ownership migration to complete successfully")
	}

	for _, contract := range []string{
		"      - ./images:/root/images\n",
		"      - ./automation-scripts:/root/automation-scripts:ro\n",
		"      - ./connected-sources:/root/connected-sources:ro\n",
		"      - ./agent-workspaces:/root/agent-workspaces\n",
		"      - ./phase2-feeds:/root/phase2-feeds:ro\n",
		"      - phase2-control-state:/root/phase2-control-state\n",
		"      - ${CLOUDQUERY_SUMMARY_HOST_DIR:-./cloudquery-summary}:/cloudquery-summary:ro\n",
		"    read_only: true\n",
		"      - /tmp:rw,noexec,nosuid,size=64m\n",
	} {
		if !strings.Contains(backend, contract) {
			t.Errorf("backend Compose contract lost %q", strings.TrimSpace(contract))
		}
	}
	volumeStart := strings.Index(backend, "    volumes:\n")
	if volumeStart < 0 {
		t.Fatal("backend volume block is missing")
	}
	volumeEnd := strings.Index(backend[volumeStart:], "    # The API writes")
	if volumeEnd < 0 {
		t.Fatal("backend volume block has no boundary")
	}
	wantVolumes := "    volumes:\n" +
		"      - ./images:/root/images\n" +
		"      - ./automation-scripts:/root/automation-scripts:ro\n" +
		"      - ./connected-sources:/root/connected-sources:ro\n" +
		"      - ./agent-workspaces:/root/agent-workspaces\n" +
		"      - ./phase2-feeds:/root/phase2-feeds:ro\n" +
		"      - phase2-control-state:/root/phase2-control-state\n" +
		"      - ${CLOUDQUERY_SUMMARY_HOST_DIR:-./cloudquery-summary}:/cloudquery-summary:ro\n"
	if got := backend[volumeStart : volumeStart+volumeEnd]; got != wantVolumes {
		t.Fatalf("backend mount set changed unexpectedly:\nwant:\n%s\ngot:\n%s", wantVolumes, got)
	}

	for _, contract := range []string{
		"    command: [\"/app/app\", \"migrate\", \"up\"]",
		"      IMAGE_SAVE_DIR: /tmp/images",
		"    read_only: true",
		"      - /tmp:rw,noexec,nosuid,size=64m",
	} {
		if !strings.Contains(migration, contract) {
			t.Errorf("backend migration Compose contract lost %q", contract)
		}
	}
	if strings.Contains(migration, "    volumes:") {
		t.Fatal("backend migration service must not gain host or secret mounts")
	}
	for _, contract := range []string{
		"    user: \"0:0\"",
		"    entrypoint: [\"/usr/bin/chown\"]",
		"    command: [\"-R\", \"--no-dereference\", \"10001:10001\", \"/root/phase2-control-state\"]",
		"    restart: \"no\"",
		"    mem_limit: 64m",
		"    cpus: 0.10",
		"    pids_limit: 16",
		"    network_mode: none",
		"    read_only: true",
		"      - no-new-privileges:true\n",
		"    cap_drop:\n      - ALL\n    cap_add:\n      - CHOWN\n",
	} {
		if !strings.Contains(permissions, contract) {
			t.Errorf("state ownership migration contract lost %q", strings.TrimSpace(contract))
		}
	}
	if strings.Contains(permissions, "    environment:") || strings.Contains(permissions, "    env_file:") || strings.Contains(permissions, "    secrets:") || strings.Contains(permissions, "    networks:") {
		t.Fatal("state ownership migration must not gain environment, secrets, or network attachments")
	}
	permissionVolumeStart := strings.Index(permissions, "    volumes:\n")
	permissionVolumeEnd := strings.Index(permissions, "    read_only: true\n")
	if permissionVolumeStart < 0 || permissionVolumeEnd <= permissionVolumeStart {
		t.Fatal("state ownership migration volume boundary is missing")
	}
	if got, want := permissions[permissionVolumeStart:permissionVolumeEnd], "    volumes:\n      - phase2-control-state:/root/phase2-control-state\n"; got != want {
		t.Fatalf("state ownership migration must mount only the named control-state volume:\nwant:\n%s\ngot:\n%s", want, got)
	}
	embedBytes, err := os.ReadFile("../migrations/embed.go")
	if err != nil {
		t.Fatalf("read migration embed contract: %v", err)
	}
	if !strings.Contains(string(embedBytes), "//go:embed pre/*.sql post/*.sql") {
		t.Fatal("migration SQL must remain embedded in the backend binary")
	}
}
