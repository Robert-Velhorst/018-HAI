# Original large test-log triage

Sixteen original backend JSONL logs were previously outside the inventory's
2 MiB hashing limit. This pass read each file with an 8 MiB bound, rejected
symbolic links, verified the inventoried size and checked read stability.
It recorded SHA-256 and Git-blob identities without changing the original files.
These are first content fingerprints, not proof of their contents at an earlier
checkpoint where only their sizes had been recorded.

The files total 109,561,042 bytes and contain 471,639 valid structured Go test
records. No malformed lines or non-event JSON records were found. Raw `Output`
values, fixture identifiers and private local paths are not copied into this
public report or treated as implementation patches.

Seventy distinct top-level test names had historical failure events. Sixty-nine
still have a unique declaration across fourteen current test files. This is
source-presence evidence, not execution proof or proof that the old failures
remain. Subtest names are excluded from that count. No raw log, current-pass
claim or removal authorization is inferred from those name matches.

The remaining name is
`TestFrameworkHealthAcceptsFreshRegistryEvidenceWithoutTreatingInventoryAsProbe`.
Current health-contract tests instead reject registry declarations and legacy
or relabeled verification flags as live probe evidence, while retaining a
separate trusted in-process fixture contract. That is not a simple name match
or demonstrated semantic equivalence. Do not restore a historical acceptance
rule merely to recover its test name; review the evidence and intended trust
boundary first.

The logs remain historical evidence requiring further diagnostic/context review.
Parsing all records does not mean every `Output` body has been semantically
reviewed, every failure fixed or all related source changes integrated. The
current CI and focused tests must be evaluated separately at their exact commit.
No source checkout, log, archive, database or worktree was removed.
