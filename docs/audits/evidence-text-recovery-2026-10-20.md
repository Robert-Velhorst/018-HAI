# Evidence text preservation recovery

## Retained behavior and reproduced gap

The retained verification normalizer preserved Unicode and internal punctuation.
Current main replaced every non-ASCII-letter/digit character with whitespace.
This collapses distinct accented names, currencies, project identifiers, version
separators, non-Latin names and URL structures into identical comparison keys.

New regressions were run before the fix through the actual memory-promotion
write boundary using synthetic repositories. All six different-text fixtures
incorrectly stored one fact each. A matching non-Latin-only sentence failed its
positive comparison check. These are reproduced local failures, not evidence of
an incident in a deployed account.

## Change

Recover the retained conservative normalization in the existing helper: allow
case, whitespace, wrapping quotes and terminal sentence punctuation to vary,
while preserving Unicode and internal symbols. No dependency or alternate
verification pipeline is added. The positive non-Latin promotion case also
asserts that the original claim text is preserved in the stored fact.

Current owner-bound run/source checks, durable evidence identifiers, freshness
and weekday checks, confidence thresholds and high-risk approval restrictions
remain unchanged. Older optional repository interfaces and persistence helpers
are not copied over the stronger current contracts.

## Boundaries

The complete local native `verification` and `memory` package suites passed after
the fix. The new cases cover rejected collisions, matching Unicode, formatting
compatibility and an actual preserved non-Latin memory write. No frontend or
dependency changed. Hosted exact-head CI and configured database integration
remain separate gates; no local database or provider stack was started.

This is the source-sentence gate for durable memory promotion, not a general
semantic-equivalence engine, proof of LLM factual correctness, or live-provider
acceptance. Existing case-insensitive matching remains part of the contract;
this patch does not introduce accent folding or Unicode canonicalization.

The retained source still matches its inventoried hash. No original source or
remaining archive is removed by this patch, and complete reconciliation of the
other local differences is not claimed.
