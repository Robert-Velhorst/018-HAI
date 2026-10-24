# Current Python parent contract candidates

The separate requirements-port.in metadata candidate adds explicit distribution
ports for Datasets 5.1.0+hai.fsspec.1, Mbstrdecoder 1.1.5+hai.chardet7.1,
Pytablewriter 1.2.1+hai.typepy2.1 and Typepy 2.0.0+hai.dateutil3.1. They target
Fsspec 2026.9.0, Chardet 7.7.0, Typepy 2.0.0 and python-dateutil 3.0.0 on the
pinned Python 3.15 profile. Upstream Typepy 2 still excludes Dateutil 3, so it
also needs an explicitly identified port; updating Pytablewriter alone is
insufficient. DataProperty 1.1.1 already permits Typepy below 3.

All 274 upstream Python files, resources, modes, licenses and entrypoints are
preserved. The deterministic repacker verifies original RECORDs and complete
source manifests, changes only exact version/dependency/interpreter metadata
plus build provenance, and regenerates the distribution record. Two independent
static builds have matching wheel hashes. These local versions identify modified
distributions and do not assert upstream support.

The public metadata job builds and hash-checks all four parent wheels before
resolving the candidate graph in the existing restricted SDK. Canonical LM
runtime inputs/locks, Dockerfile, application and existing cases are unchanged.
No vendor module is imported locally. Current-source resolution, real SDK
imports, unchanged vendor functional cases, image/native security and full
model/data/privacy acceptance remain unqualified. No resolver override,
advisory exception, validation stub or compiler/interpreter guard bypass is
introduced. Optional upstream feature declarations remain unchanged; their
graphs/behavior require separate qualification before use.
