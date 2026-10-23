# Artifact component reconciliation

## Scope and decision

Reviewed the complete retained OpenClaw artifact component and its complete
component specification, then compared their changes against the current
implementation. This reconciles two retained source variants, not the entire
frontend, repository, worktree or transcript archive.

No older component behavior needs to overwrite current production code. Retain:

- Owner-scoped metadata loaded only after expansion; file bytes require an
  explicit download or retention action.
- Bounded metadata, digest, size, date and quota validation; unknown file size is
  distinct from zero, and malformed metadata is not an empty success.
- Explicit, version-bound deletion confirmation with the least destructive
  initial focus. A changed saved-copy fingerprint invalidates confirmation.
- Mutation ownership independent of a closing component, with authoritative
  server-state refresh before reporting an uncertain operation as confirmed.
- Separation of execution identities when a view changes during a mutation.
- Bounded downloads, checksum validation, cancellation, generated filenames and
  object-URL cleanup. Source content is not rendered as trusted HTML or followed
  as an arbitrary upstream URL.
- Explicit distinction between encrypted retention and deliverable verification.

Keep the newer implementation's stable disclosure target, accessible modal
label, metadata-collection status, retained-content quota wording and
allow-listed encrypted-copy integrity failure. An unreadable saved copy must be
preserved rather than described as deleted. A stale-version response refreshes
the list and requires a new decision, not an automatic removal retry.

## Related integration

PR #297 corrects the shared mutation service rather than duplicating that logic
in this component. It validates responses before success delivery, registers
in-flight operations before synchronous observer callbacks, and prevents an
errored observable from restarting its HTTP mutation on resubscription.

That PR remains a separate integration dependency until its exact-head checks
and merge are verified. A component-level review does not establish that those
service changes are on main.

## Preservation boundary

All 32 current component cases passed in Chrome Headless with Angular HTTP test
fixtures. This exercises rendered confirmation and disclosure controls, mutation
reconciliation, execution changes, cancellation, checksum rejection and safe
failure messages. It is not an end-to-end live-account or deployed-app test.
This reconciliation changes documentation only; it does not claim a new
production build or repeat PR #297's separate service verification.

Original source copies remain intact. The private reconciliation ledger binds
review decisions to exact retained source content and current Git blobs.
Publication, tests and semantic review do not by themselves authorize deleting
the originals: post-merge preservation and the remaining source scope must also
be verified. This review does not establish archive cleanup eligibility or live
Gateway, provider, installation or operator acceptance.
