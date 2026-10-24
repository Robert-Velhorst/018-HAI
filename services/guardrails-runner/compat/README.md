# Guardrails AI offline compatibility port

This profile keeps upstream Guardrails AI 0.11.0's validation engine on Python
3.15.1. It does not replace Guardrails with a generic Pydantic-only validator.
The original Runner, Pydantic schema and schema-validator modules remain
byte-identical; all 34 Guard method source hashes are checked against upstream.

Five source-bound patches defer package/telemetry optional exports, make a
LangChain type import conditional, initialize offline settings without reading
operator configuration, and avoid creating telemetry SDK/exporter objects when
metrics are disabled. The wrapper uses a request-local Guard with zero reasks;
SDK call inputs are not kept in a global cross-request history. Fixed schema,
request/reference bounds, digest/response behavior and engine identity remain.

`build_port_wheel.py` performs a deterministic, source-hash-bound ZIP transform.
It verifies the upstream wheel SHA-256, original/patched module identities and
unchanged validation methods, preserves Apache-2.0 licensing and regenerates
RECORD. The local wheel version `0.11.0+hai.offline.1` identifies modified source;
it is not an official upstream release. The builder imports no vendor code.

The supported surface is HAI's existing offline fixed-schema profile. Provider
calls, Hub installation, optional CLI integrations and telemetry exporters are
outside qualification. Their unconditional dependency chain is removed from
this derived profile rather than importing vulnerable or prerelease-only
packages. All 23 direct core dependency minima were checked against public
stable metadata and declare Python 3.15 support. The complete generated graph
still requires advisory/version review and actual hosted SDK/image qualification.

The metadata job builds the public port wheel, checks it against the locally
reviewed deterministic digest, and resolves its input inside the existing
restricted unprivileged SDK. Only public input/wheel/tool/cache/output paths
are mounted. No app source, credentials or Docker socket are mounted.

Preparation remains a draft: canonical runtime input/lock/image are not yet
switched to this port. The inherited Guardrails input/lock mismatch remains a
strict failing gate until the verified graph and image are integrated. Existing
application cases remain unchanged; no new cases or local application suites
are authored/run. Static ZIP/AST/RECORD checks are not runtime compatibility or
network-isolation proof. No main merge or deployment is authorized by this work.
