# Brain skill source reconciliation

## Scope

Review the full meaningful differences in twelve selected present files from
the retained artifact worktree against main commit
`30781d4fb393386aecab0c449d100d158f4eabf5`. Compare original raw hashes separately
against the private inventory. No missing retained behavior was identified;
keep the newer implementations and tests rather than restoring older consent
or input-handling omissions.

## Per-file decisions

Paths below are relative to `backend/internal/`.

| File | Decision |
| --- | --- |
| `brain_skill_selection/service.go` | Keep catalog fingerprints on decisions, exact reviewed-catalog consent before enable, current-fingerprint checks on returned/stored decisions, reapproval for legacy or changed catalogs, complete catalog-entry comparison and valid UTF-8 identity checks. Disable remains possible without a fresh enabling review. |
| `brain_skill_selection/owner_store_adapter.go` | Keep full server catalog matching and reviewed-catalog validation before writes; reject inconsistent returned catalog/owner/actor state. |
| `brain_skill_selection/repository_postgres.go` | Keep the catalog fingerprint in owner-scoped latest-decision projection. |
| `brain_skill_selection/service_test.go` | Keep stale-review rejection before persistence, legacy-decision reapproval, source/license/display metadata mismatch rejection, UTF-8 identity checks and explicit task exclusions/contrast corrections. |
| `brain_skill_selection/owner_store_adapter_test.go` | Keep current catalog review in the batched owner-state fixture without restoring per-skill repeated reads. |
| `brain_skill_selection/http_task_integration_test.go` | Keep the inventory/preview catalog fingerprint and authority boundary through authenticated HTTP consent and task guidance; retain concurrent latest-decision response checks. This is local fixture acceptance, not a real account test. |
| `brain_skill_selection/repository_postgres_integration_test.go` | Keep the dedicated test DSN, explicit connected database identity, current catalog consent, opposing-write fingerprint checks and append-only ledger assertions. Do not run against a general production/test DSN. |
| `brainskills/catalog.go` | Keep the complete canonical catalog fingerprint, bounded HAI-authored skill-design and communication guidance, explicit owner review boundaries, Unicode-aware matching, expanded MCP name matching, bounded contrast work and request exclusions. |
| `brainskills/catalog_test.go` | Keep source/license/guidance pins, fingerprint order stability and source/license sensitivity, bounded advisory guidance assertions, multilingual intent/negation cases and contrast-limit tests. |
| `brainskills/handler.go` | Keep no-store owner responses, bounded guidance plus authority previews, exact reviewed guidance/catalog fingerprints when enabling, server-resolved skill pins, stale-review conflicts and strict JSON field validation. |
| `brainskills/handler_test.go` | Keep no-store assertions, fingerprint forwarding, source-pin changes despite unchanged guidance, current-preview consent flow, strict request validation and reapproval-safe output. |
| `brainskills/selection_service_http_test.go` | Keep the reviewed catalog fingerprint through the service-backed selection mutation and latest safe response. |

## Authority and provenance

Skills provide bounded HAI-authored prompt guidance. They do not grant tools,
runtime execution rights, external messaging authority, paid calls or autonomy.
Communication guidance produces a reviewable draft; skill-design guidance is a
proposal requiring review, not permission to edit the catalog automatically.

Catalog consent is sensitive to the complete reviewed metadata, including
source and license identity, even if the HAI guidance text is unchanged. Older
enabled decisions without that catalog binding require reapproval. Source
hashes in the catalog are pin metadata, not proof of a fresh upstream fetch in
this reconciliation; no upstream skill repository was fetched or executed.

Request-level exclusions and later corrections remain bounded matching logic,
not proof of universal natural-language interpretation. Task guidance still
requires current owner consent and never replaces execution policy.

## Verification and retention

Both packages passed locally on Windows with Go 1.27.3:

```text
go test -p 1 -timeout 180s ./internal/brainskills ./internal/brain_skill_selection -count=1
```

All twelve original raw hashes matched the frozen inventory. The PostgreSQL
integration-tagged suite was not executed, and no live account/provider or
rendered UI acceptance is claimed. Hosted exact-head CI remains required.

Original worktree versions remain intact. This semantic review does not prove
that their historical bytes are preserved on main. Other source versions,
ignored files, archives and runtime/user data still require their own review
before any deletion. No such material was deleted in this batch.
