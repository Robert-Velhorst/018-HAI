# Verification source reconciliation

## Responsibilities behind the removed names

The complete original/current delta of
`backend/internal/verification/service.go` was read through its final hunk.
Six original declaration names flagged by structural triage map to replacement
responsibilities rather than six automatically missing features:

| Original declaration | Current responsibility |
| --- | --- |
| `persistFinalization` | `persistCompletedVerification` with transaction-aware evidence/claim/completion writes |
| `persistEvidenceAndClaims` | The persistence closure inside that finalization method |
| `runForOwner` | Required repository owner lookup and explicit owner check in `runDetailsForOwner` |
| `normalizedEvidenceSentence` | `normalizeEvidenceSentence` and the current sentence parser |
| `singleWeekday` | Single-day sets inside `hasConflictingSingleWeekday` |
| `isStaleEvidence` | Trimmed, case-insensitive stale-state check in claim evaluation |

The current context-aware source reads, exact raw/extraction provenance checks,
review-required Trello handling, owner identity requirements and stricter memory
promotion thresholds are retained. No wholesale old-file restoration is used.
Changed sentence splitting and legacy-owner migration behavior are not claimed
semantically identical merely because responsibilities can be mapped.

## Recovered relevance guard

An original guard had been weakened: the newer candidate selection accepted a
0.20 lexical overlap without requiring two meaningful shared tokens. Source
quality could then raise the combined score enough to label an unrelated claim
`source_supported`.

The restored guard requires at least 0.22 meaningful overlap and two shared
meaningful tokens before source quality participates in ranking. Contradiction
screening also uses the original 0.22 relevance floor. Current stopword handling,
provenance checks, contextual cancellation and approval gates remain.

Regression cases demonstrated both false-support outcomes before the fix: one
shared meaningful word at 0.25 overlap and two shared words at 0.20 overlap.
Both now remain unsupported, without attached source references, while an
actually matching snippet retains source support. This is a conservative lexical
filter, not an LLM entailment check or proof that a statement is true.

## Finalization acknowledgement

The current finalization method had ignored the value returned by `UpdateRun`.
Five repository-response fixtures demonstrated that missing, foreign-run,
foreign-owner, unfinalized-status and changed-answer acknowledgements could
still return a completed-looking result and create long-term memory.

Finalization now requires a nonempty run identity and a returned acknowledgement
matching every run field except database-managed creation/update timestamps.
The expected data is captured before calling storage, so mutation of the input
cannot redefine the expected acknowledgement. A mismatch returns an error from
inside the transaction. The transaction fixture confirms rollback of evidence,
claims and the completion audit; memory promotion does not start.

For nontransactional compatibility repositories, earlier successful writes
cannot be promised rolled back. No automatic retry is introduced. A matching
acknowledgement is still a storage contract, not independent confirmation that
an external database has committed correctly.

## Actual verification and remaining scope

Both regression groups failed before their corresponding fixes. Afterward the
complete native Go verification package passed: 152 named test/subtest pass
events, zero failures and zero skips, observed through `go test -json -count=1`.
Dependencies were offline and build/temp paths were on D:. Formatting and Git
whitespace checks passed. These are local results, not live provider, mail,
database-restart or semantic-entailment acceptance.

Further review still includes creation/per-record storage acknowledgement
binding, secondary audit failure reporting, external provider/source acceptance
and remaining source variants. The broader router/extraction leads and three
declaration ambiguities from the Go inventory remain open. This checkpoint does
not authorize removing the original checkout or transcript archives.

A later [creation-boundary follow-up](verification-creation-boundary-2026-10-23.md)
addresses the creation-acknowledgement item with independent input snapshotting
and fail-closed validation. The preceding test count describes this document's
original checkpoint, not that later revision.
