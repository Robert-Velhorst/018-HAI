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
