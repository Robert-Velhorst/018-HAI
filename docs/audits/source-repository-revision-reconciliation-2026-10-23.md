# Source repository revision reconciliation

## Retained-source review

Read the complete retained artifact-worktree repository, the complete difference
to the model-maintenance variant and the complete difference from that variant
to current code. This reconciles two physical repository variants, not the whole
source service, all database acceptance tests or the retained transcript archive.

Keep current optimistic source updates instead of the older unconditional save.
Retain exact owner/connector/version matching, revoked-source refusal, atomic
manual history/queue creation, terminal replay and active-work checks, atomic
cursor/history completion and bounded owner-scoped history. Keep PostgreSQL
advisory-lock cleanup, source-before-extraction lock order, correction-worker
fences, transactional index/lesson/correction removal, final-effect audit and
post-commit projection boundaries. Do not restore weaker older deletion paths.

Keep one-use, expiring owner-bound OAuth state, revocation serialization and
token-snapshot-bound reconnect failures. A failure for an old token must not mark
a replacement grant stale or revive an inactive source.

## Revision correction

Both reconnect-status update paths assigned application wall-clock time to
`updated_at`, unlike ordinary source updates. Equal-resolution timestamps or a
clock moving behind the previous revision could therefore reuse or decrease the
revision used by optimistic updates.

Use the existing PostgreSQL monotone revision expression for these status
changes too: the later of database wall-clock time and the prior revision plus
one microsecond. Source/token ownership, status filters and lock scope remain
unchanged. No schema or provider permission change is required.

## Verification and remaining gates

- Both dry-run SQL construction cases failed before repair; the full uncached
  source package and Go vet pass afterward.
- Two dedicated PostgreSQL cases cover mark/clear reconnect and exact-token
  failure, using an ahead-of-clock starting revision and stale-update rejection.
  They use the existing dedicated-database guard and rollback fixture.
- CI selects both cases explicitly and requires each top-level PASS marker.
  The workflow contract failed before wiring and all 115 contracts pass after;
  workflow YAML and six migration shell blocks parse successfully.
- The PostgreSQL cases compiled but skipped locally because their dedicated DSN
  was absent. Actual SQL behavior is pending CI, not locally verified.

Original worktrees and archives remain intact. Publication and semantic
reconciliation are not evidence that every historical source byte is preserved
on main or that a whole worktree can be deleted. No live Google account, provider
refresh, database service or installed runtime was changed during this review.

## Main conflict reconciliation

Integrated main at `add36afdf7067dade10b2a7277e254b9f452d41b` without
dropping the newer workflow-proposal PostgreSQL selection or its per-test PASS
checks. The OAuth revision loop remains separately selected and checked. The
combined CI contract suite now passes all 117 cases, and workflow YAML plus all
six migration-job Bash blocks parse. The full uncached source package passes
again. No local database DSN was supplied; dedicated SQL acceptance remains a
new-head CI gate after this merge-conflict resolution, not a local test claim.

## Verified CI follow-up

Both dedicated PostgreSQL cases subsequently reported explicit PASS markers in
the migration job of [CI run 39921916717](https://github.com/Robert-Velhorst/018-HAI/actions/runs/39921916717/job/120308922793).
The run's head was verified as `3e49e8322bb1065e858b2f8d97280e9ced51c006`,
and all 41 exact-head checks completed successfully before squash-merging PR
#315. This establishes the selected reconnect-revision SQL fixture behavior on
PostgreSQL 18.6, not live Google OAuth, mail ingestion or full provider acceptance.

Post-merge comparison found three exact PR files and two shared CI files with
reviewed additions only. The additions retain newer Trello boundary compilation,
bootstrap-secret checks and maintenance projection tests without removing the
OAuth selection or its PASS requirements. The remote PR branch and all local
originals remain retained because shared-file preservation is not an exact
five-file byte match. The broader recovery and cleanup gate remains open.
