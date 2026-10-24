# Garak and Evidently dependency refresh — runtime candidate

This follow-up starts at HAI Python refresh #337, source
`fe3c8a734ba7f1a737a1c49ab702190d29b562c8`. That proposal and the separate
approval heads are preserved. No main merge or deployment is included.

The current public metadata selects OpenAI 3.37.0, NLTK 3.10.4, Markdown
3.11.2, Pandas 3.1.0 and Setuptools 84.2.1. Evidently 0.7.23 is still its
latest stable release. Garak's engine advances from 0.15.1 to 0.17.1 in a
separate hashed engine lock. Runtime graph locks exactly mirror their legacy
requirements.txt files. Docker and CI consume the same dependency locks.

The successful isolated metadata run 40083420926 generated 46 Garak-profile
packages and 81 Evidently packages at source 45c2e3a. Both retained artifact
ZIPs match GitHub's published digests. Their input hashes, SDK identities,
generated lock hashes and checkout Git tree were verified before import.
The imported locks, exact SDK images and engine lock are now integrated into
Docker/CI. Garak's container build also requires its installed CLI to load.
Fresh source-matching contracts/builds/health checks remain required; metadata
resolution does not qualify the runtime.

The 105 distinct engine/application/build package versions were checked against
current stable PyPI metadata and per-version advisory feeds. None was a
prerelease or carried an advisory in that feed. Plotly remains at 5.24.1 under
Evidently's parent constraint instead of current stable 7.1.0. This is a version
gap, not a claim that the native images or runtime are vulnerability-free.

## Metadata and interpreter limits

The first wheel-only Python 3.15.1 resolutions failed: Langdetect 1.0.9 has no
usable wheel, and PyYAML has no usable wheel for that target. No source package
code was executed on the operator machine. A separate wheel-only Evidently
resolution for Python 3.14.9 produced 81 packages, but uv warned that its local
interpreter selection used 3.14.6. It is metadata evidence, not runtime proof.

Dynaconf 3.3.5, the current release used by Evidently, declares Python
`>=3.10,<3.15`. Its runner therefore targets the newest public 3.14 patch,
3.14.9, while Garak targets Python 3.15.1. The Evidently vendor constraint
remains a gap against the full latest-Python objective; it is not called
complete or silently overridden.

The dedicated metadata workflow resolved both graphs in digest-pinned official
SDK containers. The nonroot, read-only, capability-free containers receive only
public requirement inputs and a verified uv binary, with disposable output and
cache mounts. No repository application source, credentials, Docker socket,
provider configuration or operator database is mounted. Public package build
backends can execute inside those containers to supply metadata. The workflow
retains actual generated locks, logs, input hashes, SDK identity and source
revision for review before import. It runs no HAI application suite itself.

## Garak profile boundary

The existing runner deliberately installs only the fixed HTTP generator and
prompt-injection probe profile using --no-deps for the engine. Many upstream
provider/local-model dependencies are unconditional distribution requirements,
not optional extras; the profile does not satisfy the full distribution's
dependency declarations. The verified 0.17.1 wheel declares OpenAI >=2,<3,
while this profile advances its already-3.x SDK to current 3.37.0. The selected
generator's static source still uses OpenAI(base_url, api_key) and
client.chat.completions, but static inspection and CLI startup do not establish
provider/binding compatibility. That upstream constraint conflict remains
unresolved; no pip-check or complete vendor compatibility claim is made.

## Evidently compatibility with Pandas 3.1

At source 400643d the Garak job passed, but Evidently's existing contracts
failed during import: upstream's legacy data_integrity_tests module imports
infer_dtype_from_object from a private Pandas module removed in Pandas 3.1.
Pandas remains at current stable 3.1.0. No cases or assertions are removed.

The runner now carries a dtype adapter built on public Pandas/NumPy APIs and
an explicit SDK-only installer that changes that single vendor import. It
retains legacy datetime aliases, scalar dtype handling and specific-unit
rejection. It does not add a symbol to Pandas globals. The installer accepts
only Evidently 0.7.23 with original module SHA-256
`a2754fa739125076375915530483ba6c38ec3035197db82e8a20a83e3b84f0d1`,
or that exact module with the reviewed import already applied; other code or
versions fail closed. The patched module SHA-256 is
`f5ccfd3827869c952760149d263c3adece9ad71032bcc7b614125da0ce0b7784`.
The installed distribution RECORD is updated for the changed file and CI/build
logs retain original/patched and adapter hashes. This is an explicit local
compatibility patch, not a claim that upstream shipped an unchanged fix.

The installer runs in disposable CI SDKs and Docker builds before the existing
contracts, not on the operator interpreter. Local verification only parsed
AST/YAML, checked the exact wheel source/one-import transformation and retained
existing application/case blob identities. Fresh hosted report execution,
image and complete source qualification remain required. Full upstream legacy
behavior beyond HAI's existing coverage remains unverified.

References: [affected upstream import](https://github.com/evidentlyai/evidently/blob/v0.7.23/src/evidently/legacy/tests/data_integrity_tests.py),
[previous Pandas dtype handling](https://github.com/pandas-dev/pandas/blob/v3.0.6/pandas/core/dtypes/common.py),
[public dtype conversion](https://pandas.pydata.org/docs/reference/api/pandas.api.types.pandas_dtype.html).

This does not resolve the other HAI runner graphs, existing SQLitedict advisory,
all native image findings, provider acceptance or deployment boundaries.

Primary records: [Garak](https://pypi.org/pypi/garak/json),
[Evidently](https://pypi.org/pypi/evidently/json),
[Dynaconf](https://pypi.org/pypi/dynaconf/json), and the official
[Python SDK manifest](https://github.com/actions/python-versions/blob/main/versions-manifest.json).
