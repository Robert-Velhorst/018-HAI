# Scanner and WASI interpreter update

The Gitleaks, Grype, Syft and WASI images select CPython 3.15.1 on Debian 13
through an immutable Python image digest. Their builds apply available Debian
updates and retire pip and its bundled bootstrap wheels. Gitleaks, Grype and Syft
upstream image manifests are also pinned by verified registry digest.

Public stable releases checked on 24 October 2026:

| Component | Selected release | Primary source |
| --- | --- | --- |
| Python | 3.15.1 | https://www.python.org/getit/source/ |
| Gitleaks | 8.30.1 | https://github.com/gitleaks/gitleaks/releases/tag/v8.30.1 |
| Grype | 0.121.0 | https://github.com/anchore/grype/releases/tag/v0.121.0 |
| Syft | 1.55.0 | https://github.com/anchore/syft/releases/tag/v1.55.0 |
| Wasmtime | 50.0.0 | https://github.com/bytecodealliance/wasmtime/releases/tag/v50.0.0 |

Wasmtime's Linux x86-64 archive is checked against its official release-asset
SHA-256. Its health label records the selected version. Version 50 makes WASIp0
imports opt-in, so the existing module command explicitly uses `-Spreview0` to
preserve that previously available interface. The supported flag adds P0 imports
to the same WASI context as P1; it adds no directories, inherited environment or
module arguments. Token authentication, admitted filename/digest checking,
timeout, standard-stream handling and fixed environment remain unchanged.
See the pinned [CLI option source](https://github.com/bytecodealliance/wasmtime/blob/v50.0.0/crates/cli-flags/src/lib.rs)
and [linker implementation](https://github.com/bytecodealliance/wasmtime/blob/v50.0.0/src/commands/run.rs).
Actual legacy-module behavior still requires hosted acceptance.

Seven wrapper CI rows now select Python 3.15.1. DeepTeam retains its separate
vendor compatibility constraint. Gosec and Whisper image interpreters still
require their own update and qualification; changing their SDK CI rows does not
establish their installed image versions.

For the four updated images, CI records the real interpreter, fixed binary
version, OpenSSL and SQLite versions, installer absence, verifier digest and
image identity. The inventory command runs without network, as UID 10001, with
a read-only root filesystem and bounded resources. It mounts only the dedicated
verification script. Existing application cases and health checks remain.

Version inventory is not complete binary-module security, real source scanning,
private advisory-database compatibility or WASI isolation acceptance. The
aggregate scanner contracts and operator-supplied advisory data require their
existing acceptance process. No new application cases or operator data changes
are introduced by this proposal.
