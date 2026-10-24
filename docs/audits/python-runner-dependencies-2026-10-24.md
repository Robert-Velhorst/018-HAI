# Python runner dependency refresh — 24 October 2026

This draft refreshes FastMCP, DeepEval, Deepteam, Docling and LM Evaluation Harness requirements. It does not establish that all HAI components are current, fully scanned or deployed.

## Changes

- FastMCP and fastmcp-slim: 4.1.3. Setuptools in the four affected graphs: 84.2.1. Docling: 2.142.1. DeepEval: 4.3.1. LM Evaluation Harness keeps its current 0.4.13 release and resolves NLTK 3.10.4.
- Regenerate complete hashed graphs; keep requirements.txt byte-identical to requirements.lock. Container installation now consumes the same lock used by CI, including Docling, whose earlier Dockerfile independently resolved a smaller package list.
- Pin four runner images and CI SDKs to Python 3.15.1. Deepteam 1.0.10 explicitly requires Python <3.14, so its existing runtime line advances to the current 3.13.17 patch. Migration to Python 3.15 remains unresolved for that package.
- Preserve the official PyTorch CPU index and CPU variant. Public index selection matches pip highest-version behavior; generated hashes bind the selected artifacts. No private index or credential was introduced.
- Hash-lock Cython, setuptools, wheel and setuptools-scm build metadata tools. Pyclipper declares all four in its source pyproject.toml; omitting SCM metadata returned the incorrect version 0.0.0. Disable automatic isolated build dependency downloads during application dependency installation. Remove build-only tools afterward; retain wheel where the application graph actually requires it.
- Preserve existing application code, cases, environment privacy flags, non-root users, health checks and endpoint behavior. Update two existing CI policy cases to the current Docling pin and canonical hash-lock install path, retaining all case names and assertions on CPU wheels, hashes and trusted CA versions. Existing hosted CI remains required.

## Local preparation evidence

The five application graphs contain 380 package entries. Application and build locks together contain 219 distinct name/version pairs. Resolved version metadata and per-version PyPI advisory feeds were inspected. All selected releases have non-yanked files.

Dependency metadata resolution used pinned uv 0.13.5. Wheel-only resolution first failed on PyYAML/SQLitedict source distributions. Resolution then succeeded in an existing, digest-pinned Python 3.15.1 SDK container with only public requirements, owned outputs and public caches mounted. Dependency build-backend metadata could execute there; no application source, provider credentials or private repository content was mounted. No host SDK, application suite or new case was installed/executed locally.

Local checks cover YAML/Python syntax, complete hash blocks, mirror equality, selected direct versions, retained existing cases and whitespace. These checks do not qualify package imports, native compilation, container health or real providers. Fresh exact-head hosted results must establish those boundaries.

## Unresolved security and version gaps

- SQLitedict 2.1.0 remains the latest public release and still carries GHSA-g4r7-86gm-pgqc / PYSEC-2026-1939. No fixed version is recorded. Keeping its dependency does not remediate the advisory; replacement/integration is required. No ignore or exception was added.
- opentelemetry-semantic-conventions 0.66b1 has no non-yanked stable release. Its prerelease dependency remains an integration gap.
- The following 12 selected package versions remain behind the newest public stable releases under parent constraints. Declared marker/extra conditions still require activation review; the table is source metadata, not a claim that every listed conditional constraint is active.

| Package | Resolved | Newest stable | Declared parent restrictions excluding newest |
|---|---|---|---|
| chardet | 6.0.0.post1 | 7.7.0 | `mbstrdecoder 1.1.5`: `chardet<7,>=3.0.4` |
| click | 8.3.3 | 8.5.0 | `deepeval 4.3.1`: `click<8.4.0,>=8.0.0`; `scipy 1.18.1`: `click<8.3.0; extra == "dev"` |
| fsspec | 2026.7.0 | 2026.9.0 | `datasets 5.1.0`: `fsspec[http]<=2026.7.0,>=2023.1.0` |
| huggingface-hub | 1.33.0 | 2.3.0 | `docling-ibm-models 4.0.4`: `huggingface_hub<2,>=0.23`; `docling-slim 2.142.1`: `huggingface-hub<2,>=0.23; extra == "all"`; `docling-slim 2.142.1`: `huggingface-hub<2,>=0.23; extra == "models-local"`; `docling-slim 2.142.1`: `huggingface-hub<2,>=0.23; extra == "standard"` |
| mpmath | 1.3.0 | 1.4.1 | `sympy 1.14.0`: `mpmath<1.4,>=1.1.0` |
| python-dateutil | 2.9.0.post0 | 3.0.0 | `typepy 1.3.5`: `python-dateutil<3.0.0,>=2.8.0; extra == "datetime"`; `typepy 1.3.5`: `python-dateutil<3.0.0,>=2.8.0; extra == "test"` |
| rich | 14.3.4 | 15.0.0 | `deepeval 4.3.1`: `rich<15.0.0,>=13.6.0`; `httpx 0.28.1`: `rich<14,>=10; extra == "cli"` |
| semchunk | 3.2.5 | 4.1.1 | `docling-core 2.104.0`: `semchunk<4.0.0,>=2.2.0; extra == "chunking"`; `docling-core 2.104.0`: `semchunk<4.0.0,>=2.2.0; extra == "chunking-openai"` |
| tabulate | 0.9.0 | 0.10.0 | `deepeval 4.3.1`: `tabulate<0.10.0,>=0.9.0` |
| typepy | 1.3.5 | 2.0.0 | `pytablewriter 1.2.1`: `typepy[datetime]<2,>=1.3.2` |
| typer | 0.26.8 | 0.27.3 | `docling-core 2.104.0`: `typer<0.27.0,>=0.12.5`; `docling-core 2.104.0`: `typer<0.25.0,>=0.15.1; extra == "dclq"`; `docling-slim 2.142.1`: `typer<0.27.0,>=0.19.0; extra == "all"`; `docling-slim 2.142.1`: `typer<0.27.0,>=0.19.0; extra == "cli"`; `docling-slim 2.142.1`: `typer<0.27.0,>=0.19.0; extra == "service-client"`; `docling-slim 2.142.1`: `typer<0.27.0,>=0.19.0; extra == "standard"` |
| websockets | 16.1.1 | 17.3 | `docling-slim 2.142.1`: `websockets<17.0,>=14.0; extra == "all"`; `docling-slim 2.142.1`: `websockets<17.0,>=14.0; extra == "service-client"`; `docling-slim 2.142.1`: `websockets<17.0,>=14.0; extra == "standard"`; `openai 3.37.0`: `websockets<16,>=13; extra == "realtime"` |

The remaining HAI runners, native libraries, Go/npm proposals, operator databases and deployments retain their own unresolved acceptance gates. No all-components-current or clean full-image claim is made.

## Primary sources

- [FastMCP metadata](https://pypi.org/pypi/fastmcp/json)
- [Setuptools metadata](https://pypi.org/pypi/setuptools/json)
- [Deepteam metadata and Python constraint](https://pypi.org/pypi/deepteam/json)
- [SQLitedict advisory](https://github.com/advisories/GHSA-g4r7-86gm-pgqc)
- [Python SDK versions](https://github.com/actions/python-versions/blob/main/versions-manifest.json)
- [Official Python image tags](https://hub.docker.com/_/python)

## CI action freshness follow-up

The initial refreshed source `018376cb2d66d44cd6d77234512b890c4ccf9df0`
passed all 41 reported checks, including build/contracts/container health for
all five changed runner graphs, with a matching tested/source tree. That
receipt belongs to that exact head. The follow-up updates both Python setup
action references from 7.0.0 to the newer public stable 7.1.0 (released October
22), pinned at `6d4fd22b20465ccc0f7354476588e8df7c9ca0e0`. The existing
reviewed-action policy assertion follows that exact pin; its cases are retained.
Fresh CI is required for the follow-up head. No application code or dependency
lock changes in this follow-up, no new cases and no local suite run.
