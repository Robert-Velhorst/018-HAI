# Trello live acceptance wiring reconciliation

Both retained live-test files were read in full and compared with the complete
current diff. Their useful board sync, provenance, audit, cursor, incremental
snapshot, and read-only token assertions remain. The context-aware request
adaptation used a stale helper name: building the live-tagged source package
failed with a too-many-arguments compiler error. Calling the existing contextual
helper repairs the boundary without changing production provider behavior.

The Windows runner also omitted required owner/member identity variables and
used Go 1.25.13 while the backend requires Go 1.27. It now validates and forwards
both identity bindings and selects Go 1.27.3. It refuses simulated provider URLs,
pins the real HTTPS Trello endpoint and allowlist, restores its process
environment, suppresses raw provider output, and still rejects skipped tests.
The Go live tests independently reject an overridden simulated provider.

## Verification

- Before repair, compiling the live-tagged package failed at the token probe.
- An isolated runner execution failed before repair because the Docker boundary
  did not receive the required account binding.
- After repair, six isolated PowerShell scenarios pass: valid wiring, missing
  owner, invalid member, simulated endpoint, skipped test, and process failure.
  Docker is a function fixture; no real container or account is touched.
- The live-tagged package compiles and its provider URL boundary test passes
  without credentials or network calls. CI now runs this check and both runner
  contract scripts to prevent default builds from overlooking tagged code.

These checks do not claim a fresh live Trello run, SQL persistence, restart
recovery, or webhook acceptance. The live tests retain fake local storage; a real
provider read is separate from production persistence acceptance. No credentials
are read by the isolated checks. Original worktrees and archives remain intact;
publication is not a local deletion gate.

## Cross-platform CI follow-up

The completed backend job in run `39891656115` passed Go vet, ordinary Go tests
and live-tag compilation, then failed the launcher contract on Ubuntu because
`TEMP` was unset. The contract now uses the platform temporary path with a unique
nonexistent filename, rather than a Windows-only environment variable and a
predictable shared filename.

The failure was reproduced locally with `TEMP` removed. After repair, that
contract and all six isolated execution scenarios pass. The execution contract
now permanently exercises the unset-`TEMP` case, restores the environment, and
requires zero Docker calls for the missing-environment check. This is local
PowerShell evidence; a successful fresh Ubuntu CI run remains required.
