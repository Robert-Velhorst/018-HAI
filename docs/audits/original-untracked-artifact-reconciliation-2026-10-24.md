# Original untracked artifact reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Executed prepared checkpoint: `19695154b3cf932121a748a89861be35862b8eb5`.

## Complete read and hash inventory

All 1,578 original untracked files were read completely and hashed. Reads were stable. The 1,562 inputs with previous hashes were unchanged; 16 larger logs now have fresh hashes but no earlier hash baseline. File-size equality alone is not historical byte-equivalence proof.

Main and the prepared tree each contain different content at 69 of these paths; 1,509 paths are absent. These are comparison results, not 1,509 missing product features. The absent set includes generated logs, local agent state and recovery evidence. Sixty-four non-output source/document paths already exist on main and still need per-change disposition; five output audit documents also exist there. No blanket deletion or restoration follows from these counts.

## Historical output processing

The output subset contains 1,498 files and 125,926,882 logical bytes. Every hash was rechecked during review. There are 1,013 distinct byte contents; duplicate copies beyond one per group account for only 568,425 logical bytes. Duplicate identity is not deletion authorization.

All 420 JSON documents parse syntactically (374 objects and 46 arrays). Their field values are withheld because runtime inspection records may contain credentials or personal configuration. Syntax success is not semantic verification.

The 82 JSONL files contain 519,893 parseable records, 1 non-JSON line and no oversized skipped lines. There are 135 historical test-failure events and 19 package/process-failure events. These include repeated runs and subtests; they are not a count of current bugs or distinct tests. The malformed line is retained in its original file, not discarded.

The 60 historical exit files report 54 zero exits and six nonzero exits. They do not establish current readiness or the completeness of a run. Raw log Output payloads, local paths, agent IDs, container environment values and private artifact mappings are not exported here.

The subset also contains two database dumps, a media ZIP, a compressed control-state archive and a binary fixture. They were hashed only, not restored, opened as databases or cleared for deletion. Eleven PowerShell scripts were not executed. Other text/log/HTML artifacts were read for hashing but are not semantically cleared by this pass.

## Current focused regression evidence

A fresh offline native Go run selected three historical failure roots from the background worker and execution broker. All 18 cases across 3 roots passed, with no failures or skips:

- `TestSafeExecutionUncertainOutcomeCannotCompleteOrRetry`
- `TestPolicyDeferralDoesNotStarveOtherWorkAcrossPasses`
- `TestLocalSafeWorkerPartialOutcomeVerifierFailureIsNotUnusedAuthority`

All 32 selected package Go files and the two module inputs matched the executed prepared checkpoint. The selected package files also match reviewed main; this does not assert equality of all transitive dependency sources. Inputs were hashed again after execution. The two selected original source files remained unchanged.

The run used existing Go 1.27.3, offline modules, serial package execution and two CPU threads. Its owned build cache was cleared through native Go cleanup afterward. This removes verification overhead, not pre-existing user data. No containers, agents, app stack, real database or external provider were started.

## Remaining work and retention

The other historical failure roots have not been requalified by this three-root run. Untracked source differences, private runtime state, recovery artifacts, other checkouts and conversation archives still require their own integration and preservation decisions. All original files remain intact. This report is aggregate evidence only, not a substitute for the private raw records, main integration, publication authorization or deletion proof.
