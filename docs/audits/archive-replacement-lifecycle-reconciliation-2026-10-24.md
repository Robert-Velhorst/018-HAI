# Historical replacement lifecycles: 24 October 2026

## Verified disposition

The literal `deleted_path_reintroduced` classification covers nineteen unique
delete variants across fifteen paths: seventeen variants under recognized product
roots and two under the historical engine-control scratch root.

For each variant the original delete record was read, its content fingerprint
recomputed, and the next success-flagged patch for that exact source/path located.
In all nineteen cases that next operation is an addition. Each addition's source
record, success flag, operation and content were read, and its content hash matched
an already indexed successful-add variant. All nineteen replacements are already
in the `added_file_changed` cohort; none creates an unexplained new source cohort.

| Path group | Delete/add cycles |
| --- | ---: |
| IDP authentication service tests | 1 |
| Control Center template and styles | 2 |
| LLM policy template and styles | 6 |
| Ambient Brain template and styles | 2 |
| Task Blueprint component, template and styles | 3 |
| Workflow Engine template and styles | 2 |
| Login styles | 1 |
| External-provider reality review | 1 |
| Task-plan model interface | 1 |

These were replacement steps, not persistent instructions to remove the current
product files. The archived deleted bodies and their immediate replacements are
not byte-identical to each other or to the checked main versions. A delete record
therefore cannot serve as a standalone cleanup instruction, nor does the later
file's existence alone prove that all earlier behavior survived.

## Remaining content gate

This closes the lifecycle ambiguity, not the semantic review of the replacement
content. Review remains attached to each indexed added-file variant, including
its subsequent changes and current callers/tests. Current templates, type models
and authentication tests were not removed or replaced by older snapshots.

This pass ran only bounded archive reads, hashing, index joins and current-main
blob comparisons. It did not execute archived code, run a browser, change an
operator account, publish a transcript, or delete local source data. No archive
is made deletion-eligible by this receipt.
