# Native libraries in the Garak and Evidently images

The hosted jobs at #338 source 0e32124 passed their existing contract, image
build and health checks. Their build provenance shows Python 3.15.1 (Garak)
and 3.14.9 (Evidently), Debian 13 Trixie, OpenSSL 3.5.7 and SQLite 3.46.1.
These are observed image versions, not guesses from package declarations.

Current upstream source records select OpenSSL 4.1.0 and SQLite 3.54.1. The
OpenSSL source archive SHA-256 was rechecked against the GitHub release asset
digest; SQLite's published checksum is SHA3-256. The shared manifest explicitly
records each checksum algorithm. Exact CPython release source digests come
from the official Python image definitions for each selected interpreter.

The native draft adds one shared, restricted SDK builder and named BuildKit
context for the two existing service build contexts. It compiles OpenSSL and
SQLite from verified archives, packages their client libraries under a separate
HAI prefix, and rebuilds the exact CPython `_ssl`/`_hashlib` modules. The
OpenSSL ABI changes to version 4; installing a new command-line executable or
changing only a version string would not update Python's TLS binding.

The builder requires its marked Debian 13 container, matching interpreter and
absent output paths. It preserves upstream license notices and records source,
header, binary and local package hashes. Its fresh interpreter must report the
selected TLS/SQLite versions, load system CA roots and execute an in-memory
SQLite version query. There is no operator database or SDK operation. Runtime
Debian packages are generated locally and are not advertised as Debian-issued.

Compose and CI pass only the shared public builder/manifest as an additional
context. Application/case files, dependency locks, privacy flags and endpoints
are retained. No new application cases or local application suites are added
or run. The existing hosted contract/image/health workflow remains enabled.

## Remaining acceptance

The native builder has not run on the operator machine; local verification is
AST/JSON/YAML/Compose configuration, source-preservation and whitespace only.
Fresh hosted compilation, module ABI/CA-root acceptance, observed image versions,
complete installed-image scanning and source-tree reconciliation are required.
The prior 0e32124 successes do not qualify this new native source.

Other OS, wheel-embedded native libraries and compiler components retain their
own currency/security checks. The OS's existing compatibility libraries are
not asserted to have disappeared. This proposal does not establish every
native component current, clean provider acceptance, deployment or data
migration. No main merge, provider, billing or operator database change occurs.

Build instructions and the single source manifest live in
`services/native-python-runtime/README.md` and `sources.json`.

Primary records: [OpenSSL releases](https://openssl-library.org/source/),
[OpenSSL 4.1.0 release](https://github.com/openssl/openssl/releases/tag/openssl-4.1.0),
[SQLite downloads/checksums](https://www.sqlite.org/download.html),
[Python 3.15 image source](https://github.com/docker-library/python/blob/master/3.15/slim-trixie/Dockerfile),
and [Python 3.14 image source](https://github.com/docker-library/python/blob/master/3.14/slim-trixie/Dockerfile).
