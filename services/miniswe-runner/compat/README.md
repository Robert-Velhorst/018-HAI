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

All 2,776 upstream Python files and the original Linux ABI3 Rust bridge are
preserved. The bridge is SHA-256-bound; its `cp310-abi3-manylinux_2_28_x86_64`
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
local wheel without importing vendor code.

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
