# Current-main OpenClaw migration parity check

Verified source revision: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This adds current-main behavioral evidence for part of the separately archived
Ngrok/Go review history. It does not establish complete parity for all 48 paths
or clear the shared clone/worktree for deletion.

## Source and execution boundary

Six versioned main files were materialized through Git into a dedicated input
directory: the existing migration test, initializer, installer support, startup
script, Windows-executable validator and example environment template. No product
source was edited. The main initializer/support/test differ from the prepared
PR versions, so this result is specifically main evidence, not an implied test
of those unmerged modifications.

The existing `scripts/test-openclaw-maintenance-migration.ps1` completed
successfully on Windows. It uses synthetic temporary environment files, overrides
the environment/install-root lookup, creates isolated mutexes and mocks worker/
startup registration paths. Hidden, waited-for PowerShell fixture children are
part of the existing suite. No application or Docker stack is launched.

## Observed coverage

The successful suite covers explicit maintenance opt-outs, existing token
preservation, UTF-8 encoding/BOM retention, restrictive ACL verification before
secret bytes are written, malformed-setting rejection, concurrent migration
locking, isolated installer-mutex exclusion, mocked legacy-worker replacement
guard/retry, silent-upgrade ordering and fresh-install deferral.

In particular, the existing main regression for an explicit opt-out with the
retired `HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED` key passes: the obsolete setting is
removed while the false opt-out, token and unrelated configuration remain. This
is a fixture result; no real operator environment file or account is migrated.
The result does not prove all installed-worker, reboot, provider or production
startup scenarios.

All six source-input hashes are unchanged after the run. The suite completed
its guarded cleanup of its own temporary fixtures. Its status output does not
contain a real operator token or configuration file.

## Remaining temporary input and integration gates

A separate attempt to remove the six extra materialized source inputs was
rejected by execution policy before that cleanup command ran. Read-only follow-up
confirms that the input directory remains, with six unchanged files totaling
215,208 logical bytes. No alternative deletion route was attempted. This small
retained copy must not be reported as removed or used to claim that local HAI
cleanup is complete.

The old review commits remain safely published under their verified archivetag,
but full main-parity checks and shared-repository ownership checks remain open.
PR #319 publication/rewrite and PR #332 merge decisions remain separate. Current
main migration coverage alone does not authorize deleting any retained transcript,
database, source archive or shared Git directory.
