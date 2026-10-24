# Archived client-service transport reconciliation

Baseline: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This review concerns ten historical service/interface variants already counted
in the TypeScript archive cohort. It does not add ten new files to the inventory
or clear their containing archives for deletion.

## Source comparison

Paths below are relative to `frontend/src/app/services/`. References identify
historical patch content, not Git commits.

| Path and reference | Source-level disposition |
| --- | --- |
| `assistant-command.service.ts`, `3349aa925b26` | Existing command transport retained; current source adds command-log retrieval. |
| `brain-catalog.service.ts`, `53075e70c76f` | Existing overview transport retained. Skill inventory, adoption, discovery, revalidation and recommendation methods are additions, not substitutes for the original overview. Added methods are not all exercised here. |
| `research.service.ts`, `fb08448e333c` | Existing status and search transports retained; an explicit probe is added. |
| `ragflow.service.ts`, `ccf622fec9da` | Status URL is factored through `apiUrl` without changing its resolved path. Probe and retrieve are additions. |
| `autogen-compat.service.ts`, `1b0d3266e920` | Import wrapping, semicolons and parameter formatting change; the migration target, workload and event request remain. This client creates a migration plan; it is not proof of a running Microsoft Agent Framework deployment. |
| `crewai.service.ts`, `a9b19a6181d5` | Import ordering and request-object formatting change; proposal request and success criteria remain. A proposal is not verified autonomous execution. |
| `llm-policy.service.interface.ts`, `b0f3f78441bf` | Historical policy, route and log signatures retained; probe, maintenance and generation-history methods added. |
| `llm-policy/llm-policy.service.ts`, `77b330e00198` | Historical request transports retained; imports and methods expand for the added interface capabilities. Maintenance execution and provider acceptance are not established by this comparison. |
| `memory-engine.service.interface.ts`, `c657b75171a7` | Historical dashboard, conversation, search and delete signatures retained; conversation import added. |
| `memory-engine/memory-engine.service.ts`, `97001d07cfad` | Historical transport methods retained; explicit conversation import added. No personal archive was sent through it during this audit. |

## Seven focused local contract checks

Current-main service source was transpiled using the installed TypeScript
compiler and instantiated with a recording HTTP test double. The restricted
loader supplied only the Angular decorator/HTTP and RxJS stand-ins needed by
these services; it did not load archived programs or make network requests.
The source comparison above used the historical files as data only.

All seven checks pass: Assistant command POST, Brain Catalog overview GET,
Research status GET, Research search POST, RAGFlow status GET, AutoGen migration
plan POST and CrewAI proposal POST. Each checks the resolved URL, HTTP verb,
one-request count and returned object. The four body-bearing cases additionally
check the original request object or relevant body values, including default
search limit, migration target/workload/events, and proposal success criteria.

A separate negative control changes only the in-memory current-source command
URL. The harness detects the wrong URL with an assertion failure, as expected.
No product source is mutated. An initial harness run used the incorrect method
name `list` for Brain Catalog; it was corrected to its actual `overview` method
before the successful run. That harness correction is not a repaired product bug.

The current backend route source also registers the original Assistant command,
Brain Catalog overview, Research status/search, RAGFlow status, AutoGen migration
plan and CrewAI proposal endpoints with authentication and capability checks.
Registration inspection is not handler execution, authorization acceptance, or
proof that a configured external runtime is available.

## Remaining gates

These are focused transport-double tests, not Angular TestBed tests, a full type
check, a browser flow, real HTTP integration, model execution or live-provider
acceptance. New service methods remain independently subject to verification.
Other changed services, components and archived requirements remain in scope.
No source deletion eligibility or complete archive integration is asserted.
