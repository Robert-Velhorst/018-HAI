# Archive coverage reconciliation

## Scope correction

The frozen index contains 58,960 patch entries, of which 58,890 are marked
successful and 70 are not. Its successful entries have 5,742 distinct patch
fingerprints. The earlier main comparison selected only 5,503 variants under
recognized worktree roots; it did not cover every successful variant.

A separate path-mapping pass accounts for the remaining 239 variants: 214 belong
to the historical HAI engine-control scratch checkout, and 25 concern temporary
browser-audit scripts. The scratch checkout no longer exists at its historical
path. Its source cannot be treated as disposable merely because the directory
is absent or because the known-worktree cohort was previously inspected.

Both product-source cohorts were compared with freshly verified main
`3b5afa697ebb0e72b01d59e3a04648a04317cd45`. Each selected archived patch was read
within indexed bounds and checked against its frozen content/diff fingerprint;
source size and modification time remained stable. No archived instruction or
code was executed, and no raw private transcript was published.

| Literal triage result | Known worktrees | Engine-control scratch |
| --- | ---: | ---: |
| Needs content review | 3,764 | 150 |
| Added file changed | 483 | 20 |
| Deleted path reintroduced | 17 | 2 |
| All new hunks present | 1,108 | 40 |
| Added file exact | 103 | 2 |
| Target absent | 24 | 0 |
| Deleted path absent | 4 | 0 |
| Selected variants | 5,503 | 214 |

These are literal classifications, not semantic-completion counts. Repeated
paths and earlier versions overlap. The 25 temporary-audit variants still need
their own disposition. The refreshed tooling distinguishes all selected
fingerprints from all indexed fingerprints; neither selected cohort alone
establishes coverage of the whole index or conversation history.

## One changed test variant

The Promptfoo test file moved from exact to changed relative to the earlier
main snapshot. Its archived content hash matches the earlier Git blob. All
three original test bodies remain byte-identical; canonical main adds the
engine/dependency-pin assertion and its imports. All four current native
contracts pass. This is not execution of the complete Promptfoo CLI, image or
real model, and does not waive its failing dependency-audit gate.

## Full source integrity and remaining gaps

A subsequent single-pass read verifies the complete SHA-256 of all eleven
remaining archives against their frozen baselines. Source size and modification
time remain stable. The two previously removed damaged duplicates are explicitly
excluded from this rescan, not counted as newly verified existing files.

The bounded JSONL scan locates 47 non-parseable records, recording byte offsets,
lengths and hashes for private follow-up. It does not repair, discard or infer
the contents of those records. No oversized record was encountered in the
eleven remaining files. Full byte integrity proves preservation of the baseline,
not that malformed baseline records or every conversation requirement have been
semantically processed.

The 70 non-success-flagged patch entries have 18 distinct fingerprints. Twenty
entries also have an identical success-flagged patch elsewhere in the index;
that is application evidence, not current-main or ordering proof. The remaining
failed patches must not disappear from scope simply because the successful-only
cohort excluded them.

The eleven remaining files have distinct first-64-KiB hashes. They are not exact
whole-file or strict-prefix duplicates of one another. Neither this prefix check,
the complete hashes, nor the patch triage authorizes deleting an archive. Source
integration, private-data handling, main preservation and safe local deletion
remain separate gates.
