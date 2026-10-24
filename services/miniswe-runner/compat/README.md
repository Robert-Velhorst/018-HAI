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
