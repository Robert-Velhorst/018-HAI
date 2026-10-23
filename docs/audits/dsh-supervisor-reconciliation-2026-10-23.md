# DeepSeek bridge supervisor reconciliation

## Reviewed scope

Reviewed the process supervisor, supervisor tests and bridge README from both
retained audit worktrees: six source versions. This is not a complete review of
the native Windows supervisor, server-side host-runtime protocol or every file
in either worktree.

Keep the current supervisor rather than restoring either older version. It
retains an explicit process-tree-termination observation through failed waits,
stop cancellation and timeouts. A successful termination call sets that internal
observation; failure retains the typed unverified-termination error. Current
tests additionally check that monitoring carries the observation into the stop
acknowledgment. Do not infer successful tree termination merely from the
absence of an error or from matching an error message string.

Retain suspended creation, final local authorization recheck, cancellation
checks before resume, whole-tree termination on failed execution, bounded output
capture and incomplete-output failure reporting. Preserve captured output and
nonzero exit codes, including the valid exit code 259, in their existing cases.

The newer retained test explicitly demonstrates that server stop can interleave
between revalidation and local Resume. Keep this limitation visible: polling
and a Windows Job Object do not provide distributed start/stop ordering or an
OS-enforced least-privilege sandbox. Direct WSL route detection does not inspect
arbitrary native wrapper or plugin behavior.

## Documentation decision

The retained READMEs describe an opt-in worker. That is superseded by the current
code and README: production configuration/startup, leasing, execution and version
verification entry points remain hard-disabled with an isolation-unavailable
error. Do not restore the old operational claim or enable execution through
environment flags. Authenticated peer verification, OS isolation, acknowledged
start/cancel ordering and native/operator acceptance remain unfinished work.

## Additional regression coverage

Add synthetic successful-wait, failed-wait and rejected-authorization cases with
a failing process Close. Verify the close error remains discoverable, an earlier
execution error is not displaced, cleanup occurs, captured output is retained
where available, and completion is not reported as success. This tests existing
error-joining behavior; it does not enable or repair the unfinished production
bridge protocol.

The full bridge Go package passed locally on Windows with uncached tests and
Go 1.27.3. These synthetic supervisor and local HTTP checks are not live DSH
execution, peer-authentication, sandbox or operator acceptance evidence.

## Preservation

Exact source fingerprints remain in the private reconciliation ledger. No
worktree, transcript, archive or installed runtime is removed by this change.
Semantic reconciliation and a PR are not main-preservation or deletion proof.
