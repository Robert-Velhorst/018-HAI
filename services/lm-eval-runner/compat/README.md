# LM Evaluation Harness: optional SQLite response-cache packaging

Upstream LM Eval 0.4.13 already imports SqliteDict lazily, inside CachingLM's
constructor; its other occurrence is type-checking-only. The evaluator constructs
CachingLM only when a response-cache path is supplied. HAI's fixed synthetic
suite supplies no such path, exposes no caller-provided command/options, and
does not retain raw generations or cache databases.

The upstream package nevertheless requires SqliteDict unconditionally. Its latest
release 2.1.0 has an unpatched insecure-deserialization advisory,
[GHSA-g4r7-86gm-pgqc](https://github.com/advisories/GHSA-g4r7-86gm-pgqc).

This explicitly modified `0.4.13+hai.optionalcache.1` wheel changes distribution
metadata so that dependency belongs to a separate `sqlite-response-cache` extra.
HAI never selects that extra; its supported graph must contain no SqliteDict.
This is not a patched SqliteDict release or an advisory waiver. Installing that
extra separately retains the upstream vulnerability and is outside this profile.

Every upstream Python file, task/resource, license and both CLI entrypoints are
preserved byte-for-byte, including upstream executable-resource permissions. No evaluation engine, model adapter or cache backend is
replaced with a stub. The application, six synthetic task cases and existing
application test cases remain unchanged. Response caching was already outside
the fixed-suite contract; no new caller option is added.

The standard-library builder verifies the original public wheel's SHA-256 and
every RECORD member, binds the complete Python source manifest, and emits a
deterministic wheel with a truthful local version and HAI_PORT receipt. It does
not import or execute upstream code. An installation verifier checks the actual
installed source manifest, CLI entrypoints, local version and SqliteDict absence
without importing the engine. Hosted metadata resolution must verify the
canonical lock graph and every selected package hash before qualification.

Keep draft until the current-source existing SDK/image/health cases and the
unchanged strict image gate are assessed. Existing wrapper cases do not prove a
real model evaluation, external-provider acceptance or complete privacy/security.
No application cases or local SDK/vendor/native/application/scanner suites are
added or run. No operator database/cache is opened, converted or removed.
