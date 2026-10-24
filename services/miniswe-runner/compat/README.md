# Mini-SWE current-SDK compatibility candidate

The current runtime installs Mini-SWE 2.4.6 with an older LiteLLM graph. Its
Filelock 3.32.7 has three advisories. Current LiteLLM 1.106.3 still excludes Python
3.15 and constrains Filelock below 4, OpenAI below 3, Hugging Face Hub below 2 and
the importlib-metadata backport below 9. A normal resolver therefore cannot select
all requested current versions for this integration.

Prepare an explicitly modified `litellm 1.106.3+hai.py315.1` candidate for standard
GIL CPython 3.15. It pins reviewed public Filelock 4.2.4, OpenAI 3.37.0, Hugging
Face Hub 2.3.0 and importlib-metadata 10.1.0. Those five metadata contract changes
are explicit; they do not assert upstream support. Hosted resolution, isolated
SDK import and subsequent application/image/security qualification are required.
No dependency conflict, advisory or native import check may be ignored.

All 2,775 unpatched upstream Python files and the original Linux ABI3 Rust
bridge are preserved. One explicitly source-bound regex import module is ported
as described below. The bridge is SHA-256-bound; its `cp310-abi3-manylinux_2_28_x86_64`
tag and non-pure wheel classification are retained. This is not a pure-Python
retag or native rebuild. The original release's compiler/toolchain currency and
complete native behavior are not qualified by preserving its binary.

Static source review checks 164 absolute dependency import sites. OpenAI's one
missing legacy Text export already has an ImportError/AttributeError fallback
to the current concrete response-text type. LiteLLM's two direct Filelock users
are optional setup CLI modules; their lock-file, timeout, mode and context APIs
are present in Filelock 4.2.4. No direct Python imports of the Hub or metadata
backport were found. This is bounded import/signature evidence, not behavioral
compatibility proof for the SDK's entire provider surface.

The builder verifies the public Linux wheel, every original RECORD entry,
Python source manifest, native bridge hash and original ABI tag. It preserves
resources, licenses, permissions and CLI entrypoints and emits a deterministic
local wheel with only the reviewed source patch, without importing vendor code.

The public metadata workflow resolves a separate candidate graph on Python
3.15.1. After successful resolution it installs only public dependencies into
an owned disposable directory. A separate read-only, non-root container with
network disabled imports the real SDK, Rust bridge, Mini-SWE default agent and
LiteLLM model class. It verifies installed identities and source hashes without
creating an agent/model, requesting a completion or running application cases.
No private application tree, credentials or operator SDK is mounted. The bundled
model-cost map is selected explicitly; forward-compatibility bypass flags are
not used.

This draft initially changes candidate preparation only. Canonical Mini-SWE
runtime locks, interpreter, Docker image, application code, existing cases and
Ollama command remain unchanged until the candidate has qualified. The existing
execution-boundary review, current native clients, full image scan, real model
session, privacy and deployment acceptance remain separate requirements. No new
application cases or local SDK/vendor/native/application/scanner execution is
authorized by these preparation helpers.

## FastUUID source build

The first candidate resolved but SDK installation failed: FastUUID 0.14.0 has
no Python 3.15 wheel and its backend tried to bootstrap Rust under noexec /tmp.
The candidate now prepares an explicitly modified 0.14.0+hai.py315.1 source
build with PyO3 0.30.1, Rand 0.10.3, UUID 1.29.0 and Maturin 1.15.0. The Rust
1.99.1 and Python 3.15.1 public SDK images are bound to immutable digests.

The source port changes six downcasts to current fallible casts, opts into the
existing Clone extraction behavior, preserves the standard-GIL module contract
and uses UUID's current ContextV1 name. UUID algorithms, bounds and existing
upstream cases are preserved. Four modified files have original/result hashes;
all other source files and modes remain unchanged. The obsolete upstream
UUID unstable cfg is removed: its current crate gates only the unselected
zerocopy feature behind it, and this profile selects public stable UUID APIs. The upstream Cargo lock is
retained separately; current Cargo generates the candidate lock in hosted CI.
That full crate graph still needs currency/advisory review before integration.

Compilation runs as UID 10001 in a read-only, capability-free public container.
Only its bounded dedicated compiler workspace is executable; /tmp stays noexec.
The pinned Python SDK is copied into owned job output and mounted read-only,
without installing an operator SDK. Native output retains a truthful
cp315-cp315-linux_x86_64 tag for this Debian 13 profile. There is no rustup
bootstrap, warning suppression or ABI-forward-compatibility environment flag.
An owned container identity and label bound timeout cleanup to this build only.

The workflow retains the wheel, all RECORD checks, source manifest, generated
Cargo lock and compiler versions. A separate network-disabled SDK phase checks
the actual installed extension hash and imports the real UUID class. A wheel
build/import proves neither complete UUID semantics nor model/privacy/security
acceptance. Canonical application inputs, cases, image and locks remain unchanged.

Primary migration guidance: https://pyo3.rs/main/migration
Current crate records: https://crates.io/crates/pyo3/0.30.1,
https://crates.io/crates/rand/0.10.3, https://crates.io/crates/uuid/1.29.0.

## CPython 3.15 regex import port

The first source-built FastUUID wheel installs successfully, but the actual
LiteLLM import then exposed removed sre_constants/sre_parse modules. The
0.14.0+hai.py315.1 native wheel and source/compiler receipts are retained.

LiteLLM 1.106.3+hai.py315.2 changes only its complexity-router config module
to import the actual re._constants and re._parser implementations directly.
CPython 3.14.9's deprecated wrappers forwarded those exact implementations;
the modules are removed in 3.15.1. All 30 function/class ASTs in that module,
including regex safety validation, remain unchanged. No fake parser, opcode
replacement, disabled validator or compatibility environment flag is added.
These remain private CPython interfaces, bound to the explicit current
interpreter profile and requiring real hosted import/runtime qualification.
The original source manifest and the ported installed manifest are separately
hash-bound. Both interpreters' official module-source hashes are recorded.

The retained 44-crate graph has six stable-version gaps (UUID's old MD5/digest
chain and Getrandom's r-efi constraint), despite zero reported public-feed
advisory IDs. The observed C compiler is Debian GCC 14.2.0-19. Compiler currency,
full native security and the complete latest-stable goal remain unqualified.

## Current C/C++ compiler SDK

The first qualified native wheel reports Debian GCC 14.2.0-19. The next
build runs in the immutable official GCC 16.2.0 Trixie SDK, with the pinned
Rust 1.99.1 and CPython 3.15.1 prefixes mounted read-only from owned hosted
output. Explicit CC/CXX and Cargo linker settings select the new compiler;
actual C/C++ versions are asserted and retained. Source, cases and crate
constraints are unchanged. Root filesystem/capabilities/non-root/noexec
boundaries remain enforced. The generated wheel and network-disabled SDK
imports need new-source hosted qualification; old-source success is separate.

Official release and image recipe: https://gcc.gnu.org/ and
https://github.com/docker-library/gcc/blob/master/16/Dockerfile.

## Current crate dependency ports

FastUUID 0.14.0+hai.py315.2 adds explicit source-bound UUID
1.29.0+hai.md5.1 and Getrandom 0.4.3+hai.refi7.1 ports. UUID's two manifests
select MD5 0.11.0, preserving its actual MD5-based UUID algorithm. Getrandom's
two manifests select r-efi 7.1.0 for its original UEFI RNG backend. Cargo's
explicit path patches select those honestly versioned modified sources, with
their complete original/result file manifests retained. Every Rust source,
existing upstream case, target-selection rule and license remains unchanged.

The prior old MD5 graph accounts for five of the six stable gaps; the old
r-efi contract accounts for the sixth. New-source resolution/build/import and
the actual complete generated Cargo graph require hosted proof; the older
44-crate audit is historical evidence. Linux SDK qualification cannot prove
the UEFI path. Static source review verifies nine referenced UEFI declarations
or modules unchanged between r-efi 6 and 7, including the entire RNG protocol
module, BootServices layout and LocateHandle/OpenProtocol signatures. Actual
UEFI cross-compilation and execution are still unqualified. No backend is
removed or replaced with an unsupported fallback, and no compiler/version
guard is bypassed. Existing upstream vector/functional cases are preserved;
their execution and full image/application acceptance remain required.
