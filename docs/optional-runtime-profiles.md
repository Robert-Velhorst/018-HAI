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
| `security-scanning` | Gitleaks, Gosec, Syft, Grype, Trivy | Named read-only subfolders under `./security-snapshots`; Grype also reads `./grype-db` | 512 MB-1 GB, 1-1.5 CPU, 128-192 PIDs | Aggregate/redacted scan evidence only; no fixes, source output, commits, or execution |
| `agent-framework-planning` | Microsoft Agent Framework runner | One canonical local model endpoint/tag | 768 MB, 1 CPU, 192 PIDs | Two fixed no-tool planning roles return one review-only structured draft |
| `crewai-planning` | CrewAI runner | One canonical local model endpoint/tag | 768 MB, 1 CPU, 192 PIDs | Two fixed no-tool planning roles return one review-only structured draft |
| `local-document-extraction` | Docling runner | Read-only `./connected-sources` and `./docling-models` | 3 GB, 2 CPU, 256 PIDs | Explicit source-folder extraction only; no upload, write, memory promotion, or action |
| `patch-proposals` | mini-SWE runner and private Ollama | Exactly one named read-only snapshot under `./mini-swe-workspaces` | Runner 1 GB/1.5 CPU; Ollama 6 GB/4 CPU | Disposable copied workspace and complete diff proposal only; no apply, commit, push, PR, or host shell |
| `event-bus` | Redpanda and nginx configuration consumer | `HAI_EVENT_BUS_ENABLED=true` plus the configured local broker topics | Redpanda 256 MB/0.5 CPU/96 PIDs; consumer 256 MB/0.5 CPU/128 PIDs | Kafka-compatible account/event delivery and gateway-config events only; no additional policy or execution authority |

The remaining profile-gated services are:

| Compose profile | Services |
| --- | --- |
| `verification` | Browser verifier |
| `local-a2a` | A2A gateway |
| `local-host-runtime` | Host runtime gateway |
| `cloud-tunnel` | ngrok tunnel |
| `optimization` | OR-Tools solver |
| `evaluation` | Evidently runner |
| `validation` | Guardrails runner |
| `typed-planning` | Pydantic AI runner |
| `mcp-bridge` | FastMCP bridge |
| `model-evaluation` | LM evaluation runner |
| `safety-evaluation` | Promptfoo runner |
| `deepteam-evaluation` | DeepTeam runner |
| `deepeval-evaluation` | DeepEval runner |
| `garak-evaluation` | Garak runner |
| `local-transcription` | whisper.cpp runner |
| `wasi` | WASI runner |
| `durability` | Temporal server, PostgreSQL, schema setup, and namespace setup |
| `compatibility` | Legacy `generic-auto` service |
| `provider-fixture` | Local provider-contract fixture (not an LLM) |
| `research-discovery` | Local SearXNG source discovery |

## Default Stack And Resource Ceiling

The default local topology has exactly nine services: seven long-running core
services (`backend`, `idp`, `frontend`, `nginx`, both PostgreSQL services, and
Redis) plus the one-shot `backend-migrate` and `backend-runtime-role` setup
jobs. The setup jobs use `restart: "no"`; they exit after their respective
operation and do not remain as idle workers. All 35 other service definitions
are profile-gated. The source-contract test checks this exact boundary, unique
container names, the absence of Compose replica/scale settings, and the distinct
IDP/application database bindings.

The `.env.example` limits sum to 2,688 MiB and 4.0 CPU across the seven
long-running core services. Summing those with both one-shot jobs gives 3,264
MiB and 5.1 CPU of per-service limits; the setup jobs run in sequence, so this
sum is not a claim that all caps can be reached simultaneously. These are
per-container upper limits, not reserved resources, measured idle use, or a
performance baseline. `.env.local` can override them. The optional
`patch-proposals` profile alone can add a 6 GiB Ollama memory ceiling, which is
why its model runner stays opt-in.

The two PostgreSQL services are intentionally separate: one owns IDP data and
the other owns HAI automation data and its restricted runtime database role.
Combining them would require a separately reviewed data migration, role/backup
plan, and cutover; a Compose-only edit cannot safely reclaim that process.
Likewise, the IDP, API, web asset server, and reverse proxy have distinct
responsibilities and are not duplicate copies of the HAI API.

The following commands inspect configuration or live resource snapshots only;
they do not start, stop, rebuild, or modify containers or volumes:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml config --services
docker compose --env-file .env.local -f docker-compose.local.yml --profile "*" config --services
docker ps --all --filter "label=com.docker.compose.project=018-hai"
docker stats --no-stream
```

`docker stats --no-stream` is a point-in-time observation. A defensible
resource baseline requires repeated samples across a representative idle window
and a separately defined workload; do not infer percentage savings from one
snapshot or from configured limits.

Agent Framework and CrewAI must use the same canonical local provider/model pair
that HAI knows through `OLLAMA_BASE_URL`/`OLLAMA_MODEL_IDS` or another supported
loopback provider profile. Their planning requests are blocked when the
endpoint and exact model do not match HAI's enabled local policy. Model
Intelligence separately requires an exact `HAI_MODEL_INTELLIGENCE_LOCAL_INFERENCE_ATTESTATIONS`
entry after the operator verifies the model is directly local and unmetered;
endpoint locality alone is not that evidence.

An Ollama endpoint may also serve Ollama-hosted cloud models. A loopback
endpoint or an `OLLAMA_MODEL_IDS` entry does not establish where inference runs;
tags ending in `:cloud` or `-cloud` are external and must use the canonical
external-provider approval and budget policy, never the local/free path.

The separate private Ollama image exists only for mini-SWE, is pinned through
`HAI_RUNNER_OLLAMA_IMAGE`, and stores model data in `runner-ollama-data`. HAI's
model-maintenance gate checks a configured model before use, records the result,
and may pull the exact Ollama tag. It does not automatically replace the Ollama
container image. Non-Ollama providers are probe-only and are never silently
upgraded.

## Safe Activation

Copy `.env.example` to `.env.local`, generate unique runner tokens, and enable
only one reviewed profile at a time. This general activation procedure does
not apply to the `local-host-runtime` profile or DeepSeek Harness; those remain
hard-disabled as described below.

### Local SearXNG source discovery

The `research-discovery` profile is disabled by default and exposes no host
port. Generate a unique `HAI_SEARXNG_SECRET`, set
`HAI_SEARXNG_BASE_URL=http://searxng:8080` and
`HAI_SEARXNG_ENABLED=true` in `.env.local`, then start only this service:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml --profile research-discovery up -d searxng
```

The backend and SearXNG share a dedicated internal network; SearXNG also joins
an egress network to query its configured search engines. Search snippets are
unverified candidates and are not automatically attached as evidence or
promoted to memory. Review SearXNG's AGPL-3.0 license and configured search
engines before enabling it. A separately managed local/private instance can
still be configured instead.

## DeepSeek Harness Host Runtime (Hard-Disabled)

The `local-host-runtime` Compose profile contains a narrow nginx gateway for
the native Windows host-runtime bridge. It is not the DSH runtime, and it is not
a sandbox. The HAI DSH adapter is integrated as a runtime architecture and
capability surface, but production task execution is deliberately unavailable.
The backend blocks host-job enqueue, lease issuance, and launch confirmation by
default; the Windows bridge's production configuration, polling, process-launch,
and version-check entry points fail closed. Existing job/audit records are not
deleted by this gate and must not be treated as evidence that a DSH task ran.

The two unmet execution prerequisites are independent:

1. **Authenticated bridge transport:** the configured endpoint is plain HTTP
   on loopback. A bearer token authenticates the worker to the gateway, not the
   gateway to the worker. A local process can still impersonate a loopback
   listener; loopback binding is exposure reduction, not server authentication.
   Verified TLS peer identity/pinning and safe token lifecycle are required.
2. **OS-enforced least privilege:** Windows Job Objects contain and terminate
   process lifecycles, but do not restrict filesystem, network, registry, or
   credential access. A DSH plugin/wrapper may also start WSL work outside the
   Windows Job Object. A real operating-system sandbox and native Windows
   negative tests are required; a dedicated workspace, environment allowlist,
   approval, or Job Object alone is not a sandbox.

Do not set `HAI_HOST_RUNTIME_BRIDGE_ENABLED`, `DEEPSEEK_HARNESS_ENABLED`, or
`DEEPSEEK_HARNESS_EXECUTION_ENABLED` to `true`, start this profile, or provide a
bridge token to try to enable execution. These configuration values do not
override the hard gate. Keep the example values disabled. Reopening this path
requires reviewed proof of both prerequisites above, plus independent
verification of the actual task outcome. A process exit code (including zero)
is not task completion; zero currently maps to `process_succeeded_unverified`
and does not set the automation's last-success state.

Before reconsidering production execution, retain native Windows evidence that
the worker denies access outside the authorized workspace and DSH state, denies
network access by default except for explicitly approved endpoints, cannot read
the interactive user's profile or credentials, and contains or blocks plugin
and WSL descendants. Separately test bridge peer authentication and token
rotation/revocation, then demonstrate source-aware independent verification of
a bounded task. Until those checks and a fresh security review pass, the
profile is not an activation path for DSH.

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
and this Compose topology. `docker compose config` proves that the topology is
syntactically resolvable; it does not build every image, preload a model,
provision an offline advisory database, parse a real document, or prove a
scanner/agent result.

A profile becomes **live-proven** only after a bounded approved run on the
target machine records:

1. the configured profile and non-secret allowlist;
2. runner health and version;
3. the exact snapshot, source folder, or model tag;
4. the HAI audit and approval record;
5. the bounded result plus verification outcome; and
6. confirmation that no host port, unreviewed mount, secret, or external effect
   was introduced.
