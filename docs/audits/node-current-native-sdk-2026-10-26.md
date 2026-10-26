# Original Node SDK with current native providers

The official current Node26.12.1 source bundles OpenSSL3.5.9 and SQLite3.54.0.
An existing application can therefore run current Node and a separate current
SQLite driver while its built-in node:sqlite still uses the older engine.
This isolated candidate investigates the ordinary shared-library build route
to OpenSSL4.1.0 and SQLite3.54.1, without changing a production recipe.

The reviewed Node release commit is
`bc2e01dcf6acd5cf7d169b5f26cf89abaaeb41f1`. Its original configure wrapper
accepts Python3.15, and configure.py provides normal shared OpenSSL/SQLite
options. No interpreter/compiler guard, source, test, assertion or original
case-selection rule is patched. The complete Git source is fetched at the
pinned tag, and its signature must validate with the exact official release
signer's public key. The baseline binary's checksums require verification
against the pinned official active-release keyring before hash validation.

The existing Ubuntu26.10 non-root native support SDK is reused unchanged.
Node gets its own SQLite build with all13 original definitions read from its
hash-bound sqlite.gyp, including percentile and RBU. A Python-oriented SQLite
profile is not silently substituted. Rust1.99.1 uses normal upstream toolchain
installation; Temporal is not disabled to avoid its compiler/dependencies.

All publisher source payloads are inventoried before/after the original
configure, make and make test-ci commands. The original TAP and C++ case reports
and failures are retained. A reported non-passing TAP case fails qualification
even if the upstream command tolerates it. Original platform/skip policies are
unchanged and remain distinct from complete platform acceptance. Actual
crypto/SQLite/Node features are compared with the signed official binary;
feature loss is not accepted. No new application case is authored.

Execution is limited to the marked non-root disposable hosted container.
Only public source/key inputs and diagnostic outputs are mounted; no operator
database, provider credentials or private application data is used. Public
source stays recoverable by its signed Git identity. Dependency trees,
compiler caches and duplicate source trees are not archived. Native diagnostics
remain available if a real compile/test gate fails.

Local work is limited to source/metadata/hash/AST/whitespace review. Current
native build/API compatibility and original SDK success require actual hosted
results. This is not a complete highest-version or clean whole-SDK claim:
retained npm11.20.0, ICU78.3, other bundled components, recursive Rust/npm graphs,
all sub-vendor/platform suites and full build-image security still require
independent review. No application uses this binary until source/native/case,
complete image/security and unchanged application contracts qualify and the
owner separately approves adoption. No main merge, deployment or data migration
is included.

The first hosted run40270847619 stopped before Node configure: the execution
image installed gnupg but omitted the separate gpgv package. Original tag
verification, consumer-specific SQLite compilation and normal Rust installation
completed before the missing executable stopped checksum-signature verification.
The image now explicitly installs gpgv; the signature check remains mandatory.
This correction does not establish Node compilation or original case success.
# Hosted process-reaping correction

The original baseline run40272188778 compiled Node26.12.1 with
OpenSSL4.1.0 and SQLite3.54.1, then failed its original CI command. Its
five not-ok cases include two cluster-primary cases that wait for orphaned
workers to disappear and both time out at120 seconds. The container originally
ran the Python coordinator as PID1, without an init process.

This separate experiment adds Docker's standard `--init` process-reaping
option while retaining UID10001, all dropped capabilities, no-new-privileges,
memory/CPU/PID bounds, the signed original SDK and original configure/build/CI
commands. It does not edit or select original cases, helpers or fixtures,
change the compiler/runtime configuration or substitute an interpreter.

The causal explanation is an inference from the original case source and
container recipe until the exact corrected worker executes. The socket error
and two TLS/X.509 failures remain unresolved, and the raw suite's713 skips
and four TODO directives remain unqualified coverage. No pass, accepted runtime,
ICU79 compatibility, image security or application/deployment/data acceptance
is inferred from this environment correction. The additional-provider draft376
remains separately failed in V8's ICU79 regexp-table generator.

References: [Docker init-process semantics](https://docs.docker.com/reference/cli/docker/container/run/#specify-an-init-process),
[exact original failed run](https://github.com/Robert-Velhorst/018-HAI/actions/runs/40272188778).
