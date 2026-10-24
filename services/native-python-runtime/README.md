# Guardrails native runtime follow-up

Reuse the unchanged source manifest and disposable native builder qualified in
HAI #340. Guardrails retains the source-derived engine, complete canonical locks,
fixed-schema behavior and all existing application cases from #345.

The builder compiles OpenSSL 4.1.0 and SQLite 3.54.1 client libraries plus the
exact Python 3.15.1 TLS/hash modules as UID 10001 in a marked Debian 13 SDK.
Only root-owned exported modules, local Debian client packages and provenance
enter the runtime. The build-stage interpreter and operator SDK are untouched.
No user database is read or migrated; its binding probe is in-memory only.

The local packages are not Debian-published packages. Other system/compiler and
embedded components retain their own currency and security requirements.
Existing exact-image scanning rejects every raw or modified finding without
an ignorefile, severity filter, unfixed exclusion or advisory waiver. Native
version provenance does not prove complete image safety. Fresh existing cases,
image/health checks, native-source provenance and full scans are required.

Compose validation supplies the named public builder context. Direct builds:

```sh
docker build --build-context hai-native-source=../native-python-runtime --file Dockerfile --tag hai-guardrails-runner:ci .
```

Final runtime images omit pip and its ensurepip bootstrap wheel after installing
all hash-locked application packages. Local work performs source/AST/JSON/YAML
checks only; no local native, image, application or scanner suite is run.
No main merge, deployment, provider or operator database change is performed.
