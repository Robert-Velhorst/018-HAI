# Garak and Evidently dependency refresh — preparation

This follow-up starts at HAI Python refresh #337, source
`fe3c8a734ba7f1a737a1c49ab702190d29b562c8`. That proposal and the separate
approval heads are preserved. No main merge or deployment is included.

The current public metadata selects OpenAI 3.37.0, NLTK 3.10.4, Markdown
3.11.2, Pandas 3.1.0 and Setuptools 84.2.1. Evidently 0.7.23 is still its
latest stable release. Garak 0.17.1 is the current stable engine; the existing
container still installs 0.15.1 until its engine lock and runtime changes are
integrated in the next preparation step.

Both input files are updated first. The committed runtime locks, Dockerfiles
and runtime CI installation paths have not yet been updated to those inputs.
Ordinary CI on this intermediate source therefore does not qualify the proposed
new graphs. Keep this draft until generated locks, engine integration and
fresh existing runtime contracts/image checks have been reconciled.

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

The dedicated metadata workflow resolves both graphs in digest-pinned official
SDK containers. The nonroot, read-only, capability-free containers receive only
public requirement inputs and a verified uv binary, with disposable output and
cache mounts. No repository application source, credentials, Docker socket,
provider configuration or operator database is mounted. Public package build
backends can execute inside those containers to supply metadata. The workflow
retains the actual generated locks, logs, input hashes, SDK identity and source
revision for review before import. It runs no HAI application suite itself.

This does not resolve the other HAI runner graphs, existing SQLitedict advisory,
all native image findings, provider acceptance or deployment boundaries.

Primary records: [Garak](https://pypi.org/pypi/garak/json),
[Evidently](https://pypi.org/pypi/evidently/json),
[Dynaconf](https://pypi.org/pypi/dynaconf/json), and the official
[Python SDK manifest](https://github.com/actions/python-versions/blob/main/versions-manifest.json).
