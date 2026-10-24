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

## IDP untracked test source disposition

All 10 untracked IDP test-file bodies were compared in full against main and the prepared source. Their 56 original root test names remain present. Whole-file equality against main was established after exactly enumerated successor edits, not inferred from name matching:

- 84 authentication calls receive an explicit context argument.
- Four files add the context import; the reset-mail fixture accepts the context-aware sender interface.
- The isolated PostgreSQL fixture uses the dedicated search-path query parameter.
- One concurrent refresh goroutine is reformatted without changing statement order.

Nine prepared files equal main after line-ending normalization. The password-reset integration file additionally retains two previously prepared fixture fixes: reload the persisted user snapshot to respect PostgreSQL timestamp precision, and use a fresh Gorm destination when checking a duplicate account. Explicit transformations verify the entire prepared file rather than waiving its differences.

For every file, deliberately changing an assertion causes the full-file comparison to reject the mutation. No production or test code was replaced by this source review. These are static successor checks, not passing Redis/PostgreSQL test execution or a guarantee about all transitive authentication behavior. The other untracked source groups and the complete working copy are not cleared for deletion.

## Script syntax and runner-smoke fixtures

All 18 original untracked scripts were parsed alongside their main and prepared counterparts: 54 checks with no parse errors. This comprises 39 PowerShell AST parses under PowerShell 7.6.5, nine Python AST parses, three Bash syntax-only checks and three Node syntax-only checks. None of the target installation, recovery or infrastructure scripts was executed.

The full isolated recovery rehearsal differs from its original only by importing the backup script with `-LibraryOnly`, on both main and prepared source. Reintroducing the old import would remove this execution boundary. The other script deltas remain subject to individual functional review; parsing alone does not clear them.

The runner-smoke program and its existing unit suite were exercised through injected fake process results: all 15 tests pass without skips. Real subprocess calls and process creation were independently forbidden during import and execution; the guard observed zero attempts. The tested program, test file and workflow match the prepared checkpoint. Source bytes and original inputs remained unchanged.

The tests cover generated resource/network restrictions, synthetic offline settings, diagnostic redaction, failure handling and cleanup-command selection. Their mocked readiness messages do not mean a runner or container was actually started. No Docker container, installer, provider request, real readiness probe or recovery action ran, and the unit result is not live-image or operational cleanup acceptance.

## Runner dependency source disposition

The four original untracked DeepTeam and Promptfoo manifests/locks were compared with main and the prepared checkpoint. Each prepared dependency file equals reviewed main after line-ending normalization. No dependency installation or runner execution occurred.

The DeepTeam lock changes from 68 to 69 named packages, with no original package name removed. Structured requirement and hash-record parsing identifies changes only to certifi, DeepTeam and sentry-sdk. DeepTeam is upgraded from 1.0.7 to 1.0.10; sentry-sdk becomes an explicit direct requirement. Every successor direct requirement resolves to its exact hash-locked version. Distribution files were not downloaded; hash-field validity is not distribution-integrity verification.

The complete Promptfoo manifest delta is accounted for: Promptfoo 0.123.1 becomes 0.125.0, Node 24 becomes 26, npm 10 becomes 12, the Linux x64 libsql binary is explicit, and the get-uri/basic-ftp override is retained. No other manifest field changes. Both root dependency and engine declarations match their lock records.

The old npm lock has 940 package slots and the successor has 660; 499 slot records differ. The removed transitive packages are not claimed to have identical functionality in the newer Promptfoo release. A static lock-graph check resolves 1593 original and 1128 successor dependency edges with no missing required targets, incompatible semantic-version ranges or unsupported range types. The explicit get-uri/basic-ftp override is checked separately. Removing Promptfoo or falsifying its version in in-memory negative controls is detected. Peer-dependency compatibility, package authenticity, install-script behavior, vulnerabilities and live provider evaluation are not proved by this check.

These four files need no old-version restoration based on dependency declarations or lock-graph structure. This disposition does not clear the original working copy or its other source, logs, recovery evidence or history for deletion.

## Remaining work and retention

The other historical failure roots have not been requalified by this three-root run. Untracked source differences, private runtime state, recovery artifacts, other checkouts and conversation archives still require their own integration and preservation decisions. All original files remain intact. This report is aggregate evidence only, not a substitute for the private raw records, main integration, publication authorization or deletion proof.
