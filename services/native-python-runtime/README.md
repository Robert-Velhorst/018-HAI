# Native Python runner runtime

The source manifest and builder are shared by the Garak and Evidently image
contexts. They build OpenSSL 4.1.0 and SQLite 3.54.1 from the recorded upstream
archives inside a marked, disposable Debian 13 SDK stage. The exact CPython
3.15.1 or 3.14.9 release source supplies replacement `_ssl` and `_hashlib`
modules linked to OpenSSL 4. Source digests, compiler/header/module hashes,
library/package hashes, actual versions and CA-root availability are retained
in `/usr/share/doc/hai-native/provenance.json` in the resulting image.

The generated local Debian packages identify their upstream sources and install
client libraries under `/opt/hai-native`. They are locally built artifacts,
not packages published by Debian. The OS package set and other embedded/native
components retain their own version/security acceptance requirements. This
change is not a complete image vulnerability or all-component currency claim.

The builder refuses execution outside `/native-build` in the marked Linux
Debian 13 container and requires build UID 10001. Only initial OS-package setup
uses root; compilation/package construction runs as an unprivileged SDK user.
The SDK includes Bluetooth headers required by the exact CPython socket header
configuration; these development headers are not copied into the runtime.
Exported CPython modules and provenance files are owned by root in the image.
The build-stage interpreter's original modules are not changed. A fresh isolated
probe explicitly loads only the exported modules and staged client libraries.
SDK and runtime layers apply available Debian package updates at image build.
No operator SDK or user database is modified. Its SQLite
binding verification uses only an in-memory connection. Source imports retain
upstream copyright/license notices. Published source checksums are verified;
no PGP-signature verification is claimed.

## Build

Compose's existing `evaluation` and `garak-evaluation` profiles provide the
additional context automatically. For a direct BuildKit build, run this from
the relevant service directory:

```sh
docker build --build-context hai-native-source=../native-python-runtime --file Dockerfile --tag hai-garak-runner:ci .
```

Use the appropriate image tag for Evidently. CI uses the same named context
and retains the existing contract/image/health checks. No application cases
were added or removed. The shared context's ignore file permits only the
builder and source manifest, without sending the whole repository.

## Qualification

Local work only parses AST/JSON/YAML and checks source/whitespace invariants;
the native builder must not be run on the operator machine. Fresh hosted
builds, observed versions, complete runtime image analysis and provider/data
acceptance remain required. This draft performs no deployment or database
migration and makes no current hosted-success claim for the native upgrade.

Primary records: [OpenSSL releases](https://openssl-library.org/source/),
[SQLite source/checksums](https://www.sqlite.org/download.html), and
[official Python image definitions](https://github.com/docker-library/python).

## OpenSSL 4 binding and CA policy

The exact CPython 3.14.9 source lacks the OpenSSL 4 removed-method guards
present in upstream CPython 3.15.1. The 3.14 build uses that same set of
preprocessor guards, so the removed deprecated version-specific SSL/TLS
constructors are unavailable. Modern TLS client/server contexts remain the
supported interface; no removed API is re-enabled. The upstream release
source stays unchanged and the selected compiler flags are recorded.

SDK probe and runtime explicitly use Debian's CA bundle and certificate
directory. This avoids depending on a prefix's missing `cert.pem` symlink.
The bounded build guard still requires a nonempty trust store and the exact
OpenSSL/SQLite versions. A diagnostic prints only its runtime provenance.
Fresh hosted qualification of these changes remains required.

Primary binding sources: CPython's tagged `v3.14.9` and `v3.15.1`
`Modules/_ssl.c`, and the [OpenSSL migration guide](https://docs.openssl.org/master/man7/ossl-guide-migration/).
