# Retained source boundary reconciliation - 2026-10-20

Reviewed against main `7f93a2229b6cfeb42a9ec1f18f1ae50a3b804017`.
This records bounded semantic comparisons, not a declaration that every local
worktree, file, transcript, or assertion has been integrated.

## Feed readers and outbound requests

Retained `accountfeed/fetcher.go` defines `readBoundedLocalFeedFile`,
`readBoundedFeedBytes`, and a custom HTTP transport. Current main instead uses
`readBoundedLocalFeed`, secure-root file opening and post-read identity checks,
and `safeFeedHTTPClient` with `dialSafeFeedAddress`.

The retained transport rejects private DNS answers and disables proxies and
redirects. Main preserves those protections and additionally requires an exact
URL opt-in for loopback fetching, rejects reserved/transition address classes,
and supports an opaque pagination cursor. The retained local reader uses separate
path validation and `os.Open`; it must not replace secure-root confinement on main.

Relevant existing coverage includes `fetcher_security_test.go`,
`TestFetchHTTPFeedAddsOpaqueCursorAndKeepsExistingQuery`,
`TestFetchHTTPFeedHonorsRequestCancellation`, and both local reader paths.
Windows may skip symlink fixtures without privileges; native suite success is
not proof that skipped cases executed.

## Task-operation context and durability

Retained `task_operation_service.go` embeds context dispatch helpers for claims,
heartbeats, completion, cancellation, and result lookup. Current main has
`task_operation_repository.go` and `operation_storage_context.go` plus bounded,
request-aware repository scoping.

Main also retains idempotent heartbeat shutdown, generation-checked completion,
durable authoritative result readback, and review-required results when storage
confirmation fails. Replacing it with the older file would discard these
behaviors, including retained execution evidence and blocked retries after an
uncertain outcome. Missing local helper names are not grounds for that replacement.

## GitHub developer acceptance

Retained workflow helpers infer successful CI from source-linked prose and URL
shapes. Main's `github_quality_evidence.go` explicitly rejects caller-controlled
URLs, labels, and claims as proof. It resolves owner-scoped connected-source
records, constrained to the workflow's canonical origin repository.

Main currently supports commit evidence, but deliberately does not attest CI
success, documentation changes, or Windows execution until the corresponding
structured records exist. Restoring the old prose-based helpers would create
false acceptance rather than recover a working integration. Those unsupported
attestations remain real product gaps, not completed features.

## Runtime-control review queues

Retained `pendingControlReviews` adapts the task repository directly. Current
`resume_review.go` uses the narrower `ControlReviewRepository` with a router
adapter. It checks owner, open status, and the exact persisted stop revision.
The durable pending-review flow therefore still exists despite the removed
private helper. Main also retains exact-run cancellation fanout and distinguishes
an abort acknowledgement from terminal verification; the older service lacks
that additional behavior.

## Non-Go file inventory

A read-only Git-index/nonignored-untracked inventory covered the 11 HAI worktrees
registered with the retained primary checkout, excluding `.go` paths assigned
to the separate Go review. Independent clones are not included in that count.
It recorded 12,399 path instances:

| Result | Path instances |
| --- | ---: |
| Exact raw Git blob matches at the same path on pinned main | 712 |
| Same path, different raw content | 9,742 |
| Present locally, path absent from main | 1,678 |
| Indexed path absent on disk | 267 |

Present entries total 253,742,000 logical bytes including duplicate copies.
No unreadable, changing, nonregular, out-of-root, or over-16-MiB entries were
reported in this pass. Ignored files, unregistered copies, archives outside those
worktrees, and Go paths outside the earlier three Go roots remain outside this
coverage. Raw differences include line-ending differences and are not counts of
missing functionality. The private manifest records source paths and hashes;
it is not published because the inventory includes local environment files and
agent/runtime state. No file contents are copied into this report.

An initial semantic review of missing-path migration candidates found:

| Retained migration number | Canonical number | Up-script comparison |
| --- | --- | --- |
| post 0004 operations owner/workspace dedupe | post 0006 | Equal ignoring line-ending whitespace |
| pre 0063 bounded life-ontology indexes | pre 0122 | Equal ignoring line-ending whitespace |
| pre 0064 completion-history projection | pre 0123 | Main additionally normalizes explicit JSON null success criteria |
| pre 0065 source OAuth state | pre 0124 | Equal ignoring line-ending whitespace |

These up scripts are not missing migrations. This comparison does not attest
their down scripts, execution against a particular user's database, or migration
version-table reconciliation. An older memory stylesheet also lacks main's
progressive-view, overflow, and visible-focus additions; it must not overwrite
the current stylesheet just because its raw content differs.

## Retention boundary

The native `accountfeed`, `task`, `workflow`, and `opscontrol` package suites
passed on the reviewed code. These local package runs are not live provider,
database, privileged-symlink, or deployment acceptance.

These comparisons justify retaining current implementations, not deleting the
entire source variants that contained the older helpers. Their other declarations,
test assertions, non-Go files, ignored data, and unregistered copies require their
own review. No deletion is performed or authorized by this report.
