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
paths and earlier versions overlap. The 25 temporary-audit variants now have a
separate static diagnostic disposition in
`archive-browser-diagnostics-reconciliation-2026-10-24.md`. Together the three
cohorts account for all 5,742 successful fingerprint lookups, not semantic
integration of all those changes. Neither product cohort alone establishes
coverage of the whole index or conversation history. The prepared Quick Capture
acceptance test still requires pinned dependencies and owned-stack execution.

The nineteen `deleted_path_reintroduced` variants across both product cohorts
are now paired with their next successful additions for the exact same archived
source/path. All are replacement cycles across fifteen paths, and every paired
addition is already in the `added_file_changed` scope. See
`archive-replacement-lifecycle-reconciliation-2026-10-24.md`. This resolves the
deletion lifecycle, not the semantic content gate on those replacements; it is
not an instruction to remove current templates, models or authentication tests.

## Go declaration follow-up

A standard-library Go parser pass prioritizes all 251 changed Go additions
across the two product cohorts. Its refined result is 39 variants whose archived
declarations/imports remain represented, 173 with changed but no absent
declaration identities, 36 with identities absent from the same file, and three
historically unparsable snapshots. These are AST prioritization categories, not
whole-file semantic completion or permission to restore malformed old code.

Five absent production-helper identities now have assertion-level successor
reviews in `archive-helper-successors-reconciliation-2026-10-24.md`: cookie
persistence, criterion-specific evidence, MCP endpoint validation, A2A endpoint
validation and A2A message parsing. The current implementations preserve or
deliberately strengthen the relevant behavior; the archived helpers must not be
restored wholesale merely because their names disappeared.

All nine historical `Test...` identities absent from their original files in
this cohort have individual dispositions in
`archive-test-successors-reconciliation-2026-10-24.md`. They comprise renamed,
expanded and intentionally superseded tests. Test fixtures/helpers and all other
changed declarations remain separate review work. These receipts do not clear
the 251 whole-file variants or make the retained archives deletion-eligible.

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

The subsequent private record diagnosis matches all 47 offsets, lengths and
hashes to truncated `compacted` records blocked in `payload.replacement_history`.
They are not malformed completed-patch events. This classification does not
reconstruct missing history or prove that every requirement in those snapshots
has been integrated.

The 70 non-success-flagged patch entries have 18 distinct fingerprints. Twenty
entries also have an identical success-flagged patch elsewhere in the index;
that is application evidence, not current-main or ordering proof. The remaining
failed patches must not disappear from scope simply because the successful-only
cohort excluded them.

All eighteen now have explicit dispositions in the failed-patch and subsequent
runtime/framework reconciliation receipts. Sixteen variants (65 entries) have
preserved or explicitly superseded source intent on the reviewed baseline. Two
variants (five entries) exposed actual approval-expiry and historical-provenance
gaps, with tested prepared repairs. Those repairs still require publication,
exact-head CI and main integration before they can support source cleanup.

The eleven remaining files have distinct first-64-KiB hashes. They are not exact
whole-file or strict-prefix duplicates of one another. Neither this prefix check,
the complete hashes, nor the patch triage authorizes deleting an archive. Source
integration, private-data handling, main preservation and safe local deletion
remain separate gates.
