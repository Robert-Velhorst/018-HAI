# Native runner expansion and image security

Reuse the unchanged native-source manifest/builder from #340 at `8f7d1f3`
for FastMCP, DeepEval, Docling and LM-Eval. Their exact Python 3.15.1 runtimes
now use digest-pinned Debian 13 Trixie, native OpenSSL 4.1.0 and SQLite 3.54.1
clients, and explicit Debian CA paths. SDK compilation uses UID 10001 and
exports root-owned runtime modules without changing the SDK interpreter.

The parent Garak/Evidently jobs pass contract, image and health checks;
matching checkout/source trees and observed native provenance were verified.
That does not qualify these four new images. Existing application/case files,
locks, CPU-only dependencies, offline/privacy controls and container commands
are preserved. Compose adds only the four named public builder contexts.

A strict CI image scan covers these four images, the two parent native images
and the Trivy SDK image. It archives the actual built image, verifies the saved
configuration/descriptor against Docker inspection, and scans that archive
without mounting the Docker socket or application/credential directories.
The scanner uses digest-pinned Trivy 0.76.1, a read-only unprivileged container
and resource bounds. OS, Python and relevant Go package inventories, exact
runtime/engine lock versions, custom native package identities and all raw or
modified findings are checked. No ignorefile entries, severity filters,
unfixed-only exclusions, VEX waivers or blanket advisory exceptions are added.
Any reported finding fails the gate. JSON inventory/identity reports are
retained even when the gate fails; absence of a report is not a passing scan.

The Trivy SDK itself moves from 0.76.0 to the current 0.76.1 release and an
exact Python 3.15.1 / Alpine 3.24 image. Both registry digests were read from
the primary registries; runtime package updates and actual CLI/interpreter
versions are logged. Existing mocked application cases remain unchanged.

This is a draft. Static AST/JSON/YAML, Compose, shell syntax and preservation
checks precede hosted execution. No new application cases or local application
or native build suites are authored/run. Full image/package findings, vendor
constraints (including the existing SQLitedict advisory), compiler versions,
other bundled native libraries and live acceptance remain unresolved until
actual evidence says otherwise. A passing native provenance probe is not a
complete image scan or proof every component is current. No main merge,
provider, deployment, billing or operator database change is performed.

Primary records: [OpenSSL](https://openssl-library.org/source/),
[SQLite](https://www.sqlite.org/download.html), and
[Trivy 0.76.1](https://github.com/aquasecurity/trivy/releases/tag/v0.76.1).
