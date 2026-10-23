# Verification memory reconciliation

## Scope

Reviewed the older artifact-audit verification service in full, then the complete
artifact-to-model-audit and model-audit-to-current service differences. This is
a review of two saved service variants, not proof that all recovery worktrees or
transcript archives have been integrated or can be deleted.

## Retained behavior

- Owner-scoped runs and explicit owner opt-in before durable memory promotion.
- Server-resolved source provenance, payload/snapshot binding and source freshness.
- Review gates for high-risk claims, uncertain extractions and Trello-derived content.
- Separate source-supported, deterministic calculation, test and approval states.
- Atomic verification finalization where the repository supports transactions.
- Persisted claim/evidence identifiers and the verified-fact writer's durable provenance checks.
- Caller-context propagation, cancellation handling, memory audit warnings and pursuit linking.
- Extractive memory promotion with current stricter confidence/quality thresholds;
  word overlap is not semantic entailment or proof of real-world truth.

## Recovered safeguards

The older variants contained narrower memory-to-knowledge handoff and broader
weekday-conflict checks that the current service did not fully preserve.

1. A successful writer return without a record or durable identifier is now a
   reported memory-write failure, not a stored fact. Semantic projection stays blocked.
2. The semantic projector receives only claims that actually passed memory promotion
   and returned a durable record. One stored claim cannot authorize projection of
   other claims that were skipped, including non-extractive paraphrases.
3. Relevant accepted evidence is checked for conflicting single weekdays beyond the
   highest-ranked source. Rejected, unused and unrelated evidence does not create
   a conflict. The existing direct-source conflict explanation remains unchanged.

The pre-existing conservative behavior that blocks projection when any memory
write fails is unchanged. No approval, provider, database or execution policy is relaxed.

## Verification boundary

Four new negative cases were observed failing before the fix: two missing memory
receipt cases, a mixed stored/skipped projection case, and an accepted secondary
source with a different weekday. They pass after the fix. Additional controls cover
rejected/unused/unrelated/matching evidence, and an answer-to-memory/projector test
confirms that conflicting sources never enter memory or semantic knowledge.

The uncached verification and memory package tests and verification `go vet` pass
locally. These are local fixtures, not live provider or PostgreSQL acceptance.
CI and the merge/preservation checks remain separate gates. Original recovery
copies and transcript archives remain retained.
