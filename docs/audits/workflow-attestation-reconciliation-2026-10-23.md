# Workflow attestation and verification repository reconciliation

## Reviewed scope

Reviewed the completion-attestation implementation and verification repository
in both retained audit worktrees: four source versions. This does not complete
the workflow service, verification engine or archive review.

Retain current completion guards: passed and validated results, accepted
verification status, no outstanding review or approval, and matching workflow
approval state. External actions require an immutable automation-launch URI;
ordinary non-executing results must not invent runtime evidence. Keep attestation
ID, owner, workflow, timestamps and content-digest binding, plus validation of
required fields and record integrity. The older artifact variant omits several
of these protections and must not replace current code.

Retain transaction-backed verification finalization and mandatory owner-scoped
record lookup. Keep bounded run history and the explicit migration-owner policy
for otherwise ownerless legacy data; do not turn legacy rows into shared data
for every authenticated account. The reviewed repository variants introduce no
missing behavior that should override these newer contracts.

## Result digest field-boundary repair

The result digest joined completion status, verification status, output and
failure reason with newlines. Because a whitespace-only failure reason is
accepted, output `result` with failure reason `\n` and output `result\n` with
an empty failure reason yielded identical pre-hash input. This is an encoding
ambiguity, not a cryptographic SHA-256 collision or proof of an authorization
bypass.

New result digests hash a versioned JSON record with separate fields. Output and
failure-reason byte slices preserve opaque bytes instead of allowing JSON's
invalid-UTF-8 replacement to merge different inputs. No external dependency or
database schema change is introduced. The enclosing attestation record digest
and existing-record validation format are unchanged; old stored result digests
remain opaque authenticated fields and do not need rewriting.

## Verification and preservation

The field-boundary regression failed before the repair. Tests cover distinct
opaque output bytes, stable repeated content and acceptance of an attestation
with the legacy result-digest encoding. Existing approval, evidence and tamper
tests remain part of the full workflow and verification package checks.
Both full packages passed locally with uncached tests on Windows and Go 1.27.3.

These are local package tests, not live provider or database transaction
acceptance. Exact source fingerprints remain in the private ledger. Original
worktrees and archive data remain retained until main-preservation and cleanup
requirements are independently satisfied.
