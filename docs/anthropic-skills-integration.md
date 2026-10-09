# Anthropic Skills Integration Contract

## Purpose and status

This document defines the intended, safe use of selected references from [`anthropics/skills`](https://github.com/anthropics/skills) in HAI. It does not authorize importing upstream files, installing software, or executing skills.

- **Source review date:** 2026-09-25
- **Pinned upstream revision:** [`33375500bcea98d610eb30ce10ac4e59b89c390d`](https://github.com/anthropics/skills/tree/33375500bcea98d610eb30ce10ac4e59b89c390d)

**HAI implementation status:** the opt-in catalog and task-context path are implemented in this checkout. HAI exposes an authenticated inventory and owner-only selection API, stores decisions as append-only audit events, and supplies only the selected HAI-authored summaries to the task-generation context. The Skills page exposes the same per-owner selection flow. This is not a claim of production readiness: automated checks must pass on the integrated checkout, and live PostgreSQL migration application, live LLM-provider behavior, and deployment remain unverified.

## Implemented HAI path

- `GET /api/v1/brain-skills/` returns the static, pinned catalog, its stable `catalogFingerprint`, and the authenticated owner's selection state. The fingerprint covers the repository/revision identity and the complete catalog metadata, including source, license, and HAI-guidance pins. It does not return upstream instruction bodies or executable assets.
- `GET /api/v1/brain-skills/:id/guidance-preview` returns the exact HAI-authored guidance and the same catalog fingerprint. To enable or reapprove a skill, `PUT /api/v1/brain-skills/:id/selection` requires both the reviewed guidance SHA-256 and reviewed catalog fingerprint. The handler and owner-scoped persistence adapter compare the fingerprint with the current catalog before appending the decision; stale review is rejected with `409 Conflict` and no selection is written. Disable remains available without review fingerprints, including from a stale page.
- Selection routes require the approval permission; the handler derives the owner from authenticated request context rather than accepting an owner ID from the request body.
- Migration `0084_brain_skill_selection_events` stores append-only decision events, including the actor and reviewed source/guidance pins. Current selection state is resolved from those events; an outdated pin requires renewed approval rather than silently remaining enabled.
- Task generation looks up only current, owner-enabled HAI guidance that matches the request. It passes the bounded HAI-authored summary as clearly labelled advisory context, records skill IDs, source commit/source and guidance hashes, and the consent-decision ID in plan metadata, and emits one success task event containing only those pins (never the guidance body). Lookup failures omit the guidance and let the ordinary task path continue.
- The inventory API exposes the pinned source and license paths/URLs, raw-file hashes, license identifiers, and current owner selection state. Task-plan pins intentionally omit upstream license/source text and HAI guidance bodies. The runner request schema remains unchanged; metadata is not inserted into the runner prompt. Invalid, stale, duplicate, or over-limit pins fail closed.
- The authenticated `/skills` page is the user control for that selection. Basic/Advanced view state remains local to the Skills module; provenance and the exact pinned source link are available in Advanced view.

The integration is deliberately prompt-guidance-only. Runtime does not fetch or parse upstream `SKILL.md` files at all: the reviewed source is represented by a fixed commit, file hash, license reference, and separate HAI-authored summary. The current six-entry catalog bounds each summary to 500 UTF-8 bytes. The downstream agent-guidance envelope independently allows at most four selected items, each no larger than 1,024 bytes, and a combined maximum of 4,096 bytes; with current catalog summaries, four items are bounded to at most 2,000 bytes. Exact catalog matching and independent task/action authorization still apply. HAI does not install Anthropic's skills, download or execute their scripts, create an MCP server, or grant the model additional capabilities.

## Selected references

Each source path below is pinned to the commit above. Recorded Git blob IDs identify the exact upstream object; SHA-256 values are over the exact raw bytes returned by GitHub's contents API for that immutable path. The license blob and SHA-256 values are independently recorded where available. The five repeated license hashes refer to identical Apache-2.0 license bytes; `frontend-design` has a distinct license file with the same Apache-2.0 terms. Every selected source and license path exists in the complete, non-truncated Git tree at the pinned revision.

| Reference | Intended HAI use | Upstream `SKILL.md` path | Git blob ID (SHA-1) | Source SHA-256 | License path and Git blob ID | License SHA-256 | HAI guidance SHA-256 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `frontend-design` | General interface-design considerations | [`skills/frontend-design/SKILL.md`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/frontend-design/SKILL.md) | `a5333457c414d20d625f307df945842c0952ecc3` | `d91970639e9f5c37682ac7ab60094d35f1c7c1f38d731bd56396563aee10c1d3` | [`skills/frontend-design/LICENSE.txt`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/frontend-design/LICENSE.txt) · `f433b1a53f5b830a205fd2df78e2b34974656c7b` | `0d542e0c8804e39aa7f37eb00da5a762149dc682d7829451287e11b938e94594` | `42afa3c5e1d970c766dcb269485b891818bbb59da43e2f609476f88f923e626d` |
| `mcp-builder` | Connector contracts, focused outputs, bounded input/resource use, explicit authorization | [`skills/mcp-builder/SKILL.md`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/mcp-builder/SKILL.md) | `8a1a77a47d141967b246adb4da4f91037578ff7d` | `0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295` | [`skills/mcp-builder/LICENSE.txt`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/mcp-builder/LICENSE.txt) · `4f881c52d1f72f4cfb720e339e2d35c3058d01a9` | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` | `f489360e858ac5cad27561867de6639a43bd2ebd529aa8db01618df020116ce0` |
| `webapp-testing` | Verify observed end-user paths and error/empty/success states | [`skills/webapp-testing/SKILL.md`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/webapp-testing/SKILL.md) | `4726215301db64a0cc4d41fc3219c61f37a30f4a` | `51b7349e77ec63b7744a6f63647e7566a0b4d2e301121cc10e8c2113af6556a2` | [`skills/webapp-testing/LICENSE.txt`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/webapp-testing/LICENSE.txt) · `4f881c52d1f72f4cfb720e339e2d35c3058d01a9` | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` | `5ea4cccd94c47ed4ae95d6597fcc17d3f7c4eb7f1de73432777b28f65c18fb96` |
| `discernment-nudge` | Surface consequential assumptions and one useful check without nagging | [`skills/discernment-nudge/SKILL.md`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/discernment-nudge/SKILL.md) | `49ebbcad692ffdf70e518087468609a5c4ab9e22` | `9191177c4a8ef11a20dace786d708506b22d43e748c71287bb823de0dc812dad` | [`skills/discernment-nudge/LICENSE.txt`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/discernment-nudge/LICENSE.txt) · `4f881c52d1f72f4cfb720e339e2d35c3058d01a9` | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` | `b2bac32da6aa70d6685f3a9cbd637d2f2b549c868e25d284cdd69b809ee4ce07` |
| `skill-creator` | Design and evaluate bounded HAI-authored guidance | [`skills/skill-creator/SKILL.md`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/skill-creator/SKILL.md) | Not recorded in the reviewed HAI catalog | `dcd4803e61e913e6fc27294184cd3a71f09f5e924ff20c8a9a20173e7b3c2bcf` | [`skills/skill-creator/LICENSE.txt`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/skill-creator/LICENSE.txt) · Not recorded in the reviewed HAI catalog | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` | `ec5ab96166c9799d4d7c16cea2ca5a7a3136e7d83f4e1d35e8601bea0709dca4` |
| `internal-comms` | Draft clear, audience-appropriate project and follow-up messages | [`skills/internal-comms/SKILL.md`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/internal-comms/SKILL.md) | Not recorded in the reviewed HAI catalog | `067b7587a344a928fc6534ef66b1bcd591fc7c26d207ea7ca3334aeb678d6475` | [`skills/internal-comms/LICENSE.txt`](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/skills/internal-comms/LICENSE.txt) · Not recorded in the reviewed HAI catalog | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` | `3681c3a3c4ff95deb57ea03dad5b0a568e851a358077970d5a757c743047ef83` |

These are source references, not imported skill packages. “Not recorded” means the SHA-1 object ID is absent from the reviewed HAI catalog; it was not fetched or independently verified during the catalog review covered by this document. The hash of an upstream source file is distinct from the hash of HAI's own adapted guidance. Hashes do not certify that the content is safe, correct, or suitable for every task.

## HAI storage and use boundary

HAI may store only short, HAI-authored adapted guidance associated with these reviewed references, together with provenance metadata such as repository, pinned commit, source path, license, and source hash. HAI must not store or distribute upstream `SKILL.md` bodies or their bundled materials as part of this integration. In particular, do not import or execute scripts, binaries, MCP servers, examples, credentials, or dependencies from the upstream repository.

If HAI supplies adapted guidance to a model, it is contextual advice only. It is untrusted and non-authoritative; it must never be treated as:

- a factual source, evidence, verification result, or user-approved memory;
- an instruction that overrides system policy, the user's request, or source-grounding rules;
- permission to access data, tools, accounts, files, networks, or external services;
- approval to send, publish, purchase, delete, change infrastructure, or otherwise act;
- an executable workflow or a request to invoke a process, script, MCP server, or tool.

The guidance may help HAI plan or review work. It cannot itself perform the work. HAI must keep consequential operations behind the existing, independent action-specific approval gates. Approval to use contextual guidance, if introduced, must not grant approval to execute an action.

Skills must not be ingested into source retrieval as evidence. They must not be cited as support for factual claims, promoted into verified memory, or used to change an action's risk, authorization, or approval state. A model's use of guidance is not proof that the resulting work is correct; ordinary source verification and tests remain necessary.

## Codex-local skills are separate

Skills available to Codex from `$CODEX_HOME/skills` (including any locally installed skill-authoring helper) are instructions for the Codex development environment. They are not HAI application code, are not automatically packaged with HAI, and do not demonstrate that the HAI backend loads or uses Anthropic skill content. HAI product integration must be verified independently in HAI's own routes, task flow, and tests.

## Why the rest of the repository is not included

Anthropic's pinned repository README says that many skills are Apache-2.0, but identifies `docx`, `pdf`, `pptx`, and `xlsx` document skills as source-available and **not open source**. Those four are excluded from this integration; their upstream skill contents and assets must not be bundled or adapted under an assumption that the repository-wide license is Apache-2.0. See the [README at the pinned commit](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/README.md).

The six selected references cover cross-cutting HAI design, connector, verification, decision-support, bounded capability-design, and communication-drafting needs. The runner's four-item limit is a per-task context bound, not a cap on the reviewed catalog. `academy-guide` is Apache-2.0 but Anthropic-Academy-specific, with less general value to HAI. `claude-api` is Apache-2.0 but prescribes Claude SDK/API-specific implementation and model details, which do not fit HAI's provider-neutral brain. These are relevance decisions, not license exclusions.

`doc-coauthoring` has a source `SKILL.md` at the pin, but no per-skill `LICENSE.txt` was present in the complete tree; it is excluded pending clear path-level license evidence. The four document-format skills (`docx`, `pdf`, `pptx`, and `xlsx`) are excluded by the upstream README's explicit source-available/not-open-source notice and their restrictive per-skill terms. This is not a claim that every other unselected skill has a non-Apache license. Any future candidate requires its own path-level license check, full asset/dependency review, relevance decision, and pinned-content review before adoption.

Anthropic describes this repository as demonstrations and educational examples, and advises testing skills thoroughly before relying on them for critical tasks. HAI must therefore treat each adapted idea as unverified until HAI-specific behavior has been tested. See the [upstream disclaimer](https://github.com/anthropics/skills/blob/33375500bcea98d610eb30ce10ac4e59b89c390d/README.md#disclaimer).

## Change and completion rules

- Do not silently follow the upstream default branch or automatically replace the pinned commit.
- For any proposed update, inspect the new immutable commit, individual skill paths and licenses, content hashes, changed assets, and applicable safety boundaries.
- Review and rewrite HAI guidance independently; do not treat a matching name or unchanged license as approval of new content.
- Keep source references, source hashes, and HAI-guidance hashes distinct and auditable.
- Do not claim end-to-end HAI integration until the backend exposes the intended metadata safely, relevant task flow consumes only HAI-authored guidance, no skill can grant or invoke tools, existing action approvals remain effective, and automated tests verify those boundaries.

## Verification and remaining release gates

The following full-suite results were recorded before this focused review (2026-09-24); they are historical baseline evidence, not claims that those suites were rerun after the current catalog changes:

- `go test -p 2 ./...` passed across the backend.
- `go vet ./...` passed, and the changed backend packages passed `go test -race`.
- `go test -tags integration -count=1 ./internal/infra` passed against a fresh, disposable PostgreSQL 17 database and exercised the full migration runner.
- `go test -tags integration -count=1 ./internal/brain_skill_selection -run TestPostgresRepositoryAndSelectionServiceIntegration` passed against that migrated database. It exercised the real PostgreSQL repository, owner isolation, selection revocation, and UPDATE/DELETE/TRUNCATE rejection.
- `npm.cmd test -- --watch=false --browsers=ChromeHeadless` passed all 531 frontend tests.
- `npm.cmd run build` passed. Angular still reports an existing Pursuits SCSS style-budget warning; the Skills integration did not introduce that warning.

The PostgreSQL database was created only for those checks and removed afterward. Those results do not mean a production database migration was applied or that a live model provider was exercised.

Focused verification after the catalog adjustment (2026-09-25, Go 1.25.13, Windows amd64):

- `go test -count=1 ./internal/agentguidance` passed. The catalog remains within the four-item runner inventory bound.
- `go test -count=1 ./internal/brainskills` passed. It checks the pinned source/license/guidance hashes, source boundary, and 500-byte preview limit.
- `go test -count=1 ./internal/brain_skill_selection` passed. It checks owner-consent selection and validates pinned source/license metadata.

These focused package runs were sequential, not parallel. They do not exercise a production database or a live model provider.

Before production use, still verify the migration against the supported PostgreSQL versions, exercise the API with the deployed identity/RBAC configuration, run a provider-backed task and inspect its audit record, and complete deployment/browser acceptance. Keep external actions behind their existing independent approvals.

## Source checks

The immutable upstream source reviewed is commit `33375500bcea98d610eb30ce10ac4e59b89c390d`, committed 2026-09-24 at 16:20:35 UTC. At the source review snapshot (2026-09-24), the official GitHub refs and compare APIs identified it as the then-current `main` head, one commit after `34040c9c568585f6929bedeaad110ad08f079624`. The commit changed exactly four files, all in the unrelated Claude API skill: `skills/claude-api/curl/examples.md`, `skills/claude-api/python/claude-api/README.md`, `skills/claude-api/shared/model-migration.md`, and `skills/claude-api/typescript/claude-api/README.md`. A recursive tree response was complete (`truncated=false`). Recorded source and license blob IDs and raw-byte SHA-256 values were verified against this pinned tree and the official GitHub Contents API; where Git blob IDs are not recorded in the HAI catalog, the document says so rather than treating them as verified. HAI guidance SHA-256 values are independently computed over the HAI-authored summaries.

The selected skills' upstream folders and available auxiliary files were reviewed for fit and assets. No upstream prompt, script, template, example, binary, dependency, or other asset is stored or executed by HAI. The pinned README retains the mixed-license warning and educational/demo disclaimer. This describes the immutable source review, not the live state of `main` after that review date; automated verification and live release gates are stated separately above.
