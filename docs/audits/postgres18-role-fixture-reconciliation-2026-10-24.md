# PostgreSQL 18 role-fixture reconciliation

## Source and disposition

The cached PostgreSQL-upgrade reference includes a digest pin for the disposable
runtime-role fixture. On reviewed main
`3b5afa697ebb0e72b01d59e3a04648a04317cd45` the image was only version-tagged;
the PostgreSQL-17 diagnostic labels and authentication setup path also remained.
The prepared source had the same gaps.

Port the historical pinned PostgreSQL 18.6 image into the prepared fixture,
correct the two diagnostic labels, and resolve authentication setup through the
container's required `PGDATA` rather than the old PostgreSQL-17 path. The shell
argument is single-quoted in PowerShell so the host cannot interpolate PGDATA.

Three regression methods are added to the existing CI contract unittest class;
they run with the existing suite rather than creating a new test stack.

## Verification

- All three new methods failed on the previous fixture, then passed on the fix.
- Native PowerShell parsed the complete fixture without errors.
- A mocked invocation executed only the authentication assignment and verified
  the Docker argument boundary, required container PGDATA and lack of host
  interpolation. No Docker executable was invoked.
- Bash syntax validation checked the extracted shell command without executing it.
- Static exact-main comparisons confirm the migration preflight's noncomment
  lines are identical to the historical version; the canonical migration guide
  retains dump, restore, Temporal, verification, marker and rollback obligations.
- Main's CI services and Compose database services retain PostgreSQL-18 images,
  correct root-volume mounts, preflight entrypoints and marker-aware health gates.
  Negative controls reject removing the health gate or fail-closed exit.
- Acceptance intentionally excludes Temporal from its explicit core-service list;
  its supported PostgreSQL services retain root tmpfs coverage.

These are targeted contract, parser and mocked-boundary checks. No database,
container, restore, provisioning, live acceptance or full CI run was performed.
The fix is prepared locally and is not claimed merged into main or deployed.
The cached clone remains ineligible for wholesale deletion.
