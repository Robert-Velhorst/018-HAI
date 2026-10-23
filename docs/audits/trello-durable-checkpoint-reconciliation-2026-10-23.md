# Trello durable checkpoint reconciliation

## Reviewed retained implementations

Reviewed `backend/internal/source/trello_durable.go` and
`backend/internal/source/trello_sync_repository.go` in full against current code.
Keep the newer canonical board/card binding, supported full-inventory pagination,
resumable stale-card verification under the shared request/retry budget,
action-card identity normalization and explicit inventory reconciliation phase.
The old limitation text must not replace current capability reporting: signed,
registered HTTPS webhooks are a separate configured capability, attachment bodies
are not downloaded, and scans remain eventually consistent.

Keep stricter page/receipt/cursor evidence, paused/revoked-source checks,
terminal-job owner-authorized resume, action replay from the earlier cycle
watermark and atomic checkpoint/inventory observation writes. The older direct
card-to-idle completion path would bypass current inventory verification.

This review does not mark the large connector/service or retained test files as
fully reconciled.

## Evidence count correction

Initial action pages could claim one more record than their stored receipts even
without a prior cursor. That allowance is only valid for an inclusive replay of
an existing cursor boundary. Six direct validator cases reproduce the invalid
acceptance across backfill, catch-up and incremental action phases. Without a
cursor, all action records now require receipts. Exactly receipted initial pages
and existing inclusive-boundary continuation tests remain valid.

## Late job checkpoint fence

The final job update previously matched only job/source IDs, so a late worker
could overwrite a stopped or rebound durable job while committing its page.
Match the still-running, unfinished job's owner and mode in the update predicate;
a mismatch returns the stale-checkpoint error and rolls back the page ledger,
checkpoint and inventory fetched-time updates in that transaction.

Legacy scheduled jobs with a SQL NULL owner remain compatible with their empty
Go owner field. This does not grant them a manual-owner identity or change the
source-owner/board checks. No additional job row lock is introduced into the
existing lock order.

This checkpoint fence does not roll back provider reads or content extraction /
workflow work performed earlier in the service, nor claim complete worker-lease
fencing. Earlier imports remain governed by their existing replay/idempotency
checks.

## Verification and preservation

- The six direct validator regressions failed before the correction and pass
  afterward, along with the full source package locally.
- The required PostgreSQL regression covers eight persisted job-state/binding
  mutations, unchanged stopped jobs and rolled-back page/checkpoint/inventory
  writes, restoration of valid running state and a legacy NULL-owner scheduled
  positive control. It is part of the existing guarded Trello PostgreSQL CI
  selection with a required PASS marker, not a new local database.
- PostgreSQL tests skip locally without their exact dedicated DSN and explicit
  opt-in. Actual SQL execution remains a CI gate; local fixtures do not prove
  live Trello/account/webhook acceptance.
- Original worktrees and archives remain intact. Review and publication do not
  establish exact main-preservation or deletion eligibility.
