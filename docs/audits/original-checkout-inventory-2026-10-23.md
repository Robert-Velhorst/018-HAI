# Original-checkout inventory checkpoint

## Broader scope remains open

Completing the selected 618 retained worktree variants does not complete the
original HAI checkout, ignored/baseline contents, recovery clones, or transcript
archives. This checkpoint starts a separate inventory of the original checkout's
changed and untracked paths; it does not replace that larger goal.

The original checkout was inspected with Git optional locks disabled. Its status
was stable across the read:

- 692 changed tracked paths: 244 present, 448 already missing.
- 1,578 untracked paths, including 1,498 under `output`.
- 1,806 regular files were content-hashed with a 2 MiB per-file limit; 16 present
  entries exceeded that bounded hashing scope or were otherwise not hashed.
- Present listed entries total 136,962,592 logical bytes. This is not the size of
  the complete checkout, Git history, or transcript archives.

The cached `origin/main` reference matched the freshly queried remote main at
inventory time. Within these hashed changed/untracked paths, no files matched its
Git blob byte-for-byte or by CRLF-to-LF conversion alone. This says nothing about
unchanged files, which are outside this status inventory. Eleven listed paths
match an exact path/content variant in the previous selected-source reviews;
that overlap is a review lead, not whole-checkout preservation proof.

Existing missing tracked paths are not new deletions performed by this audit.
They are not staged or published as intentional removals. Untracked output is
not bulk-published: it still needs classification for relevance, privacy, and
useful implementation evidence. Ignored files and files above the hashing bound
remain explicitly outside this checkpoint's content verification.

## First original-source comparisons

`backend/internal/accountfeed/permission_registry.go` was read in full alongside
its complete four-added/three-removed-line diff. Its fields, constructors, lookup,
declared read-only scopes, and unconditional `WriteAllowed=false` remain in the
current implementation.

Current `main` adds the `ReportsCredentialGrant` requirement: OAuth client setup
and legacy token presence are not represented as owner grants. Relevant bridge
definitions and existing Google grant regression assertions were read. The
normalized-feed registry is not advertised as the live provider connector;
provider-native health belongs to Connected Sources.

This file needs no redundant port from the original copy. Its raw original
content still differs from `main`, so it is not marked deletion-eligible. The
comparison does not certify the original bridge implementation, other files,
live provider access, or fresh test execution. The inventory's Git blob framing
was checked against Git's own `hash-object` on this representative source.

The complete original `backend/internal/accountfeed/bridge.go` and its original
27-added/six-removed-line delta were also read. The existing provider constants,
parsing/lookup functions, local-feed availability, assisted/import contracts,
scopes, and read-only limits remain. Main adds an owner-bound read-only Autoposter
adapter and credential-environment names without promoting configuration into a
verified provider read. Relevant current Autoposter validation/configuration/GET
contexts were inspected; no account configuration or network read was performed.

Two comments still described the registry as having no API adapter at all. They
now distinguish metadata-only Gmail/Drive/Calendar/GitHub/Trello contracts from
the opted-in Autoposter adapter. This is a comment-only correction, not a new
connector implementation or claim of live acceptance. The complete original
Autoposter-adjacent dependency graph and the remaining original source files are
not certified by these two file reviews. No weaker original grant behavior is
restored and neither original file is marked deletion-eligible.

## Runtime handler comparison

The complete original-to-current diff of
`backend/internal/agentruntime/handler.go` contains 33 added lines and one
replaced line. The additions check request cancellation after upload inspection,
before archive persistence, and immediately before the selection mutation under
the execution commit fence. The original callback dispatch is replaced by that
guarded dispatch; restoring the older handler would remove these checks.

The upload authorization, archive persistence/validation and selection contexts,
the adapter's commit fence, and the existing cancellation regression tests were
inspected. The fence takes the adapter inventory lock before the execution fence,
checks the emergency stop, and then invokes the guarded callback. Existing tests
cover cancellation before upload authorization, cancellation after approval,
cancellation during fence evaluation, and preserving an error returned after the
callback starts. This checkpoint inspected those tests; it did not rerun Go tests
or exercise a live OpenClaw installation.

This is a comparison of the retained file and its changed execution boundaries,
not a full runtime security certification. Archive persistence precedes selection
commit: cancellation after persistence is not proof that no disk write occurred,
and the checks do not promise rollback or interruption inside a started callback.
The current handler's Git blob matches the inspected `main` reference. The raw
original is different, so neither it nor the original checkout is released for
deletion under the direct-match rule.

## Ambient scheduling and service comparison

Five more original-file deltas were read in full: `ambient/scheduler.go`,
`ambient/durable_scheduler.go`, `ambient/scheduler_test.go`,
`ambient/repository.go`, and `ambient/service.go` (all under `backend/internal`).
Their current committed contents already match the inspected `main` reference;
no original-only feature in those deltas needs copying over the newer code.

The retained scheduling path adds startup-state reporting and requires both
durable-mode opt-out and explicit legacy-fallback opt-in before using the
in-process ticker. A durable startup failure still stops rather than bypassing
persistent review. The default queue poll changes from five minutes to one
minute; this is not a measured resource-saving claim. Scheduler status records
worker attachment, not continuous liveness or successful scan execution.

The repository adds batch fingerprint reads without removing the original
single-record operations. The service uses those reads, avoids writes for
unchanged proposals within the daily freshness window, and still saves semantic
changes and expired dismissal cooldowns. The common candidate store retains
owner-mismatch rejection, score/confidence filtering, cancellation checkpoints,
creation/update accounting and review behavior. Reduced database operation
counts in fixtures are not a measured 90% end-to-end resource reduction.

The service also adds a summary projection (no opportunity collection and three
recent scans), optional reuse of an owner-validated pursuit dashboard, and a
single owner-scoped pursuit visibility lookup. The full overview remains
available. The HTTP summary dispatch, owner/default resolution, default-row
migration and relevant existing owner-isolation, visibility, summary, batching,
freshness and scheduler assertions were inspected. Those tests were not freshly
executed for this comparison. Default needs are a configuration fallback, not
measurements of Robert's actual wellbeing.

These file comparisons do not certify every ambient dependency, integration,
concurrent writer, or live database behavior. The original service tests and
additional original ambient files remain a separate reconciliation task. No
original file or directory is marked deletion-eligible by this comparison.

## Original tests, configuration and assistant retention

A Go parser/scanner comparison now covers thirteen reviewed original files,
including two baseline test files outside the changed/untracked inventory.
Those two originals were independently read, content-hashed, and matched against
their original checkout's committed contents after CRLF normalization. The
other eleven retained their frozen inventory hashes. All reads checked stable
file size and modification time and rejected symbolic links.

Declaration identity retains comments, string literals and statement order.
Five analyzer self-checks cover those properties; they are not application tests.
Grouped import/constant declarations acquire different keys when expanded, so a
reported removed group was checked against the complete textual delta rather
than interpreted as removal of its original symbols. No original function was
removed in this thirteen-file comparison. Equal declarations do not prove that
their dependencies still behave identically.

| Original test file | Unchanged tests | Changed tests | Removed tests | Added tests |
| --- | ---: | ---: | ---: | ---: |
| `ambient/scheduler_test.go` | 1 | 0 | 0 | 1 |
| `ambient/service_test.go` | 17 | 1 | 0 | 11 |
| `config/config_test.go` | 3 | 0 | 0 | 2 |
| `assistant/service_test.go` | 13 | 0 | 0 | 2 |

Paths in the table are relative to `backend/internal`. Counts describe named
top-level test functions, not subtests or successful executions. The changed
ambient visibility test retains the original owner-filter assertion and adds
the batch-read fixture and operation-count assertions. Its helper changes and
the complete added test delta were read separately from declaration accounting.

`config/config.go` retains twelve original declarations unchanged; only
`validatePort` changes to reject zero as well as negative and out-of-range ports.
Both server and database initialization use that validator. The configuration
package's five current top-level tests passed locally with dependencies offline
and temporary/cache paths on D:. This is not a live database or deployed-server
check.

`assistant/service.go` retains 32 original declarations unchanged, extends
`Command`, and adds five declarations for caller-context propagation. The old
task and cycle interfaces remain supported; context-aware implementations receive
the caller context when available. Existing checks still prevent a canceled
command from starting subsequent work and retain partial results for review.
The HTTP request-context binding and relevant existing tests were inspected.
Legacy fallback engines cannot thereby be promised interruptible, and no new
execution authority, live provider access, or rollback guarantee is inferred.

The complete original/current assistant test delta is additive apart from its
expanded import group. These assistant/ambient tests were inspected, not freshly
executed for this checkpoint. All five newly compared file contents already
match the inspected `main`; copying the old files back would remove retained
checks and optimizations. Raw original differences remain recorded, and the
whole original checkout is still not deletion-eligible.

## Storage boundary

No original sources, Git history, databases, or transcript archives were deleted.
An attempt to remove only the disposable Node compile cache generated by these
test runs was blocked by execution policy before the command started. The cache
was confirmed still present; no alternate deletion route was attempted.
## Frontend entry and delivery follow-up

A separate bounded comparison read the complete original-to-current deltas for
`frontend/src/app/app.component.ts` and `frontend/nginx-custom.conf`. The app
entry retains its theme-service initialization and component structure; the
current title is a readonly HAI product name rather than the generic old value.
No old behavior needs restoration for that title change.

The frontend Nginx variant changes its listener from 80 to 8080 and adds a
frontend-only health endpoint. The canonical local gateway template targets
`frontend:8080`, and Compose checks the same port. SPA fallback, missing-static-
asset handling and release/cache rules remain in the compared file. Retain the
current port and health endpoint rather than copying the old listener back.
Frontend health does not imply backend, database or provider readiness.

The tracked legacy `gate/nginx-config/nginx.conf` still mentions port 80. The
inspected default Compose entry includes the local stack, which mounts the root
`nginx-config/nginx.conf.template`, not that legacy file. This establishes the
inspected entry point only, not absence of every other possible legacy consumer.
No delivery configuration was changed and no new browser/container acceptance
was run in this follow-up. The rest of the original non-Go variants still need
their own review; no original checkout or file is deletion-eligible from this
comparison alone.
