# Optional Runtime Profiles

HAI's recovered scanner, planning, document, and patch-proposal services are
available as explicit Docker Compose profiles. None starts with the ordinary
local stack. A profile is usable only when its matching backend feature flag,
allowlist, token or model setting, and Compose profile are all configured.

These services are bounded helpers, not additional HAI instances or independent
control planes. The Go backend remains the only policy, approval, audit,
verification, and workflow authority.

## Isolation Contract

All optional runner containers:

- publish no host port and are reachable only by service name from the backend;
- use a Docker network marked `internal: true`;
- run with a read-only root filesystem, a bounded temporary filesystem,
  `no-new-privileges`, all Linux capabilities dropped, and CPU, memory, process
  limits;
- receive no Docker socket, host user profile, repository mount, connected
  account credential, backend shared key, or cloud provider credential;
- remain disabled until the operator starts the profile and changes the related
  `HAI_*_ENABLED` value in the untracked `.env.local`;
- return a bounded proposal or aggregate report that HAI must still review.

The shared backend joins these private networks so it can call the runner. The
gateway, frontend, IDP, databases, Kafka, and Redis do not join them.

## Profile Matrix

| Compose profile | Services | Input or dependency | Maximum per service | Capability boundary |
| --- | --- | --- | --- | --- |
| `local-model` | Canonical Ollama provider | One explicitly reviewed local model tag | 2 GB, 2 CPU, 256 PIDs by default | Private HAI inference only; no host port, one loaded model, one parallel request, paid cost fixed at EUR 0 |
| `security-scanning` | Gitleaks, Gosec, Syft, Grype, Trivy | Named read-only subfolders under `./security-snapshots`; Grype also reads `./grype-db` | 512 MB-1 GB, 1-1.5 CPU, 128-192 PIDs | Aggregate/redacted scan evidence only; no fixes, source output, commits, or execution |
| `agent-framework-planning` | Microsoft Agent Framework runner | One canonical local model endpoint/tag | 768 MB, 1 CPU, 192 PIDs | Two fixed no-tool planning roles return one review-only structured draft |
| `crewai-planning` | CrewAI runner | One canonical local model endpoint/tag | 768 MB, 1 CPU, 192 PIDs | Two fixed no-tool planning roles return one review-only structured draft |
| `local-document-extraction` | Docling runner | Read-only `./connected-sources` and `./docling-models` | 3 GB, 2 CPU, 256 PIDs | Explicit source-folder extraction only; no upload, write, memory promotion, or action |
| `patch-proposals` | mini-SWE runner and private Ollama | Exactly one named read-only snapshot under `./mini-swe-workspaces` | Runner 1 GB/1.5 CPU; Ollama 6 GB/4 CPU | Disposable copied workspace and complete diff proposal only; no apply, commit, push, PR, or host shell |

Agent Framework and CrewAI must use the same canonical local provider/model pair
that HAI knows through `OLLAMA_BASE_URL`/`OLLAMA_MODEL_IDS` or another supported
loopback provider profile. Their planning requests are blocked when the
endpoint and exact model do not match HAI's enabled local policy.

The separate private Ollama image exists only for mini-SWE, is pinned through
`HAI_RUNNER_OLLAMA_IMAGE`, and stores model data in `runner-ollama-data`. HAI's
model-maintenance gate checks a configured model before use, records the result,
and may pull the exact Ollama tag. It does not automatically replace the Ollama
container image. Non-Ollama providers are probe-only and are never silently
upgraded.

## Safe Activation

Copy `.env.example` to `.env.local`, generate unique runner tokens, and enable
only one reviewed profile at a time.

For the canonical local model profile, set `OLLAMA_BASE_URL` to the exact
Compose-internal service and list only reviewed model tags:

```text
OLLAMA_BASE_URL=http://ollama-local:11434
OLLAMA_MODEL_IDS=qwen2.5:0.5b
LLM_REQUIRE_RECENT_LIVE_PROBE=true
```

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile local-model up -d ollama-local backend
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile local-model exec ollama-local ollama pull qwen2.5:0.5b
.\scripts\smoke-ollama-provider.ps1 -EnvFile .\.env.local
```

The acceptance script fails unless the provider is live, the router selects the
exact local model, generation is bounded and costs EUR 0, token usage comes from
the provider, redacted history contains the matching audit/telemetry record,
and `/readyz` remains fully ready.

For a security snapshot named `review-snapshot`:

```powershell
New-Item -ItemType Directory -Force security-snapshots\review-snapshot
# Copy only the disposable files that should be scanned.
# Set the five *_WORKSPACES values to review-snapshot and the required
# *_ENABLED values to true in .env.local.
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile security-scanning config --quiet
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile security-scanning up -d --build
```

For an optional planning or patch profile, set the relevant model ID first:

```text
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL_IDS=<reviewed-local-tag>
HAI_AGENT_FRAMEWORK_LOCAL_MODEL_BASE_URL=http://host.docker.internal:11434/v1
HAI_AGENT_FRAMEWORK_LOCAL_MODEL_ID=<reviewed-local-tag>
HAI_CREWAI_LOCAL_MODEL_BASE_URL=http://host.docker.internal:11434/v1
HAI_CREWAI_LOCAL_MODEL_ID=<reviewed-local-tag>
HAI_MINISWE_MODEL_ID=<reviewed-local-tag>
```

Then start only the selected profile:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile agent-framework-planning up -d --build
```

Use `crewai-planning`, `local-document-extraction`, or `patch-proposals` in the
same position for those capabilities. Recreate the backend after changing its
feature flags:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile <profile> up -d --build backend
```

Stop the selected helpers without deleting model data:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml `
  --profile <profile> stop
```

Do not add `-v` unless deletion of the private Ollama model volume is the
reviewed intent.

## Observability Bridges

MLflow and OpenLIT are not bundled server profiles. The repository contains
narrow client adapters only:

- MLflow can read allowlisted recent run metrics from an operator-hosted
  local/private tracking server. It cannot train, register, serve, mutate, or
  delete anything.
- OpenLIT can export one owner-triggered aggregate OTLP snapshot to an
  operator-hosted local/private collector. It cannot export prompts, source
  text, workflow records, model payloads, tokens, or credentials.

Both remain disabled until their endpoint, access, retention, deletion, and
network policy have been reviewed separately. HAI does not install either
server or claim the bridge is live because its configuration fields exist.

## Evidence Status

The repository contains the adapters, runner implementations, unit contracts,
and this Compose topology. `docker compose config` proves only that the topology
is syntactically resolvable. The canonical `local-model` profile has separate
retained Windows evidence for one `qwen2.5:0.5b` route and generation; that does
not prove other model tags, machines, scanners, planners, document extractors,
or patch agents.

A profile becomes **live-proven** only after a bounded approved run on the
target machine records:

1. the configured profile and non-secret allowlist;
2. runner health and version;
3. the exact snapshot, source folder, or model tag;
4. the HAI audit and approval record;
5. the bounded result plus verification outcome; and
6. confirmation that no host port, unreviewed mount, secret, or external effect
   was introduced.
