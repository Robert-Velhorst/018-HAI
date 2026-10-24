# Remaining runner lock refresh

CrewAI-profile, OR-Tools runtime and OR-Tools Python build graphs were resolved
inside restricted public-only Python 3.15.1 / Debian 13 SDKs. Five metadata
artifacts were verified against GitHub ZIP digests, input/lock/log hashes and
matching source/tested Git trees before importing the three new graphs.

The graphs contain 9, 9 and 12 packages respectively (28 distinct version
pairs). A current PyPI release/advisory audit found no selected prereleases or
per-version feed advisories. Protobuf 6.33.6 remains below stable 7.36.2 under
OR-Tools' `<6.34` constraint. This is unresolved; no constraint or gate was
weakened to hide it. Metadata feeds are not a complete native image scan.

Both runtime images and their CI SDK targets now select exact Python 3.15.1
on digest-pinned Debian 13 Trixie. OR-Tools has no official CPython 3.15 wheel
in the current release metadata, so its existing pinned official source build
supplies the wheel to both the image and the CI interpreter. A dedicated
artifact-export stage leaves the runtime as the default image target. CI
records the built wheel digest and retains all existing contract/build/health
checks; the build's existing package cases also remain. No new application
cases or local application suites were authored/run.

Canonical and legacy manifests match. OR-Tools' canonical compatibility lock
retains the unchanged official engine wheel hashes alongside the fresh runtime
graph; Python 3.15 actually installs the source-built wheel separately with its
hash-locked runtime/build dependencies. Fresh source-matching hosted builds and
observed version provenance remain required before qualification.

Mini-SWE remains separate: its latest LiteLLM dependency excludes Python 3.15
and requires Filelock <4 / OpenAI <3. Its existing graph still needs a reviewed
port or replacement; this change imports no unsafe Mini-SWE graph, performs no
interpreter downgrade, and does not resolve the separate execution-boundary
review. No complete image currency or live deployment claim is made.

No main merge, provider, billing, deployment or operator database changes occur.
