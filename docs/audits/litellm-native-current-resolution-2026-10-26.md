# LiteLLM complete current native dependency resolution

The current LiteLLM 1.106.4 publisher source still pins Rust 1.98.0 and carries
noncurrent native dependencies. This prepares a transparent source manifest port
and normal resolver evidence, without adopting it into the application/runtime.

The full public archive at `ea7ca16dbc3904d9e59a9505f379a2ca3091821f` is pinned
to SHA-256 `017210ac6e994de3bc007f578e0f86256a124a54a04e80cf88a5ac5d3e5e659e`.
All 14,294 publisher payloads are retained. The original complete source was
independently reconciled to Git, including 1,191 native payloads, 870 Rust files,
335 test/bench/fixture paths and 70 Python bridge case/fixture payloads. Static
test attributes are not collected/executed-case counts.

Reviewed overlays update 126 dependency version fields across 36 Cargo manifests,
plus the root toolchain and workspace minimum to Rust 1.99.1. Original dependency
aliases/default flags and all original source/cases/helpers/assertions remain.
Reqwest 0.13.5 replaces its obsolete `rustls-tls` feature with public `rustls` and
explicitly enables the formerly unconditional query/form APIs. Existing custom
ring-provider, certificate-root and verification code remains byte-identical.
Plain Reqwest clients' newer default TLS provider/trust behavior still requires
runtime qualification; this source proposal does not establish equivalence.

The hosted job verifies the whole archive and each original/proposed overlay hash,
installs the current public stable compiler using normal rustup verification,
then runs normal Cargo metadata with every workspace feature. It retains both
original and resolver-updated locks, all source identities and errors. No Cargo
patch/replace, prerelease exception, older-toolchain fallback, compiler bypass,
case filter, source/advisory exclusion or guard exception is introduced. Every
actual registry package version must match its currently available stable release
for the complete graph gate to pass; unknown queries/sources also fail the gate.

This is resolution evidence only. Native compilation, workspace/documentation
tests, original token-counter and secret-manager feature matrices, generated
contracts, ordinary/panic wheels, Python bridge suites, every target platform,
actual linked binaries, package/image scans and live deployment/data acceptance
remain required. The existing LiteLLM original import-version guard exception
remains pending and unapplied. All application/runtime locks and recipes are
unchanged. Local work was archive/hash/TOML/AST review; no Cargo, Rust compiler,
application, package manager, vendor helper or case ran locally. No merge or
manual deployment is included.
