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

The hosted job now installs the actual hash-locked candidate in an owned
disposable SDK, then mounts packages read-only and imports real interfaces
with network disabled. It checks all 70 distribution versions, all 751 engine
and 274 parent Python source files, preserved CLI entrypoints and prohibited
SqliteDict absence. Imports include the actual CLI, LocalChatCompletion class,
table formatter, decoder, detector, filesystem and datetime parser. No model
or evaluator is instantiated. Public-only writable outputs and timeout cleanup
remain bound to the job's own container IDs/labels; operator SDK execution is
refused. New-source SDK results and unchanged vendor functional cases remain
required before runtime integration or any claim of behavioral acceptance.

## Unchanged upstream behavior suites

After real imports qualify, the hosted job runs four suites against the same
read-only installed candidate: full packaged Mbstrdecoder and Typepy tests,
the entire Pytablewriter Markdown test file with its unchanged helpers, and
the Datasets filesystem/file-lock files. The 46 case/helper/license files are
bound to complete public sdist hashes or the exact Datasets commit and file
hashes. Original test bodies and assertions remain unchanged. This is the
stated selected vendor scope, not all optional Pytablewriter features or full
model evaluation/provider acceptance.

Test tools use a separate generated hash lock. Tablib is included for its
existing Markdown conversion case; LZ4/Zstandard cover all compression cases
without skipping optional codecs. Their public source builds use the pinned
GCC 16.2.0 SDK and a read-only copy of the pinned Python 3.15.1 SDK prefix.
No operator compiler or credentials are mounted. Only the dedicated compiler
workspace is executable; the case containers have read-only roots, no network,
dropped capabilities, UID 10001 and bounded CPU/memory/processes. Tool packages
may not shadow a qualified runtime package with a different version.

The harness retains JUnit, logs, exact source manifests and tool-resolution
evidence for every suite, including failures. Qualification requires observed
passing cases and zero skips, errors or failures in all four suites. A failed
suite does not prevent the other suites from producing their evidence. No
case expectation, warning filter, compatibility guard or advisory gate is
relaxed. New-head hosted results remain required.

The initial hosted suites pass Datasets 11, Mbstrdecoder 28 and Typepy 835
cases. Markdown passes 78/79; its original clear-theme case fails because the
optional upstream altrow theme is absent from the test environment. The tool
input now includes the current altrow package. Normal dependency resolution is
constrained to the exact qualified runtime graph and uses the reviewed local
port wheels; incompatible requirements still fail resolution. Overlapping tool
distributions must match both runtime versions and every Python/native source
byte before tests run. No case or production dependency is changed to make the
theme assertion pass. All four suites require new-source hosted qualification.


## Canonical runtime integration

The canonical input and both lock mirrors now select the exact 70-package graph
qualified on source 0559e08 with 953 unchanged vendor cases. Normal CI and the
image recipe build all five source-bound wheels, verify their individual hashes,
then install the hash-locked graph. The installed-profile verifier checks the
engine plus all four parents against their original Python source manifests.
The four original parent distributions retain 274 Python files unchanged.

Existing application cases, task files, native runtime profile and strict image
scan are preserved. New-source image behavior/security must be qualified by its
own hosted results; prior source imports/vendor cases do not prove deployment
readiness. No real model/provider/private-data acceptance is claimed.
