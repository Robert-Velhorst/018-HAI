# LLM policy response evidence reconciliation

## Retained-source comparison

Read the complete artifact-variant-to-previously-reviewed-model-variant deltas
for the LLM policy implementation and tests, then the complete deltas from that
baseline to current code. This extends an existing baseline review; it is not a
claim to have reread all unchanged code or finished the entire HAI inventory.

Keep current behavior rather than restoring the older policy: Ollama cloud tags
are paid/unknown even behind a local endpoint; explicit approval cannot bypass
disabled paid use; durable budget reservation precedes paid inference; audit
failure withholds completed output; estimated tokens are not measured throughput;
local-provider labels do not authorize remote hosts. Retain DNS/IP and TLS
boundaries, redirect refusal, bounded requests, response-size limits, secret
redaction, cancellation accounting, non-streaming response validation and daily
catalog checks without claims of hosted model upgrades.

## False-positive readiness correction

The current probe accepted any syntactically valid JSON as live provider
evidence. That included JSON null/scalars/arrays and HTTP-200 error objects. A
successful transport and JSON syntax alone do not establish a model-list result.

Require an object response and reject a nonempty error member before publishing
live status or model evidence. An absent, null or empty-string error member is
not itself a failure. Retain existing compatibility with empty object/catalog
responses: they establish only endpoint response, not availability of a named
model or current model artifact. Do not expose the raw rejected response.

## Preservation and acceptance

All 14 negative probe cases failed before the correction. Afterward, all eight
empty-catalog/optional-empty-error compatibility cases and the full uncached LLM
package passed; `go vet` passed. The history-to-strict-generation test also
passed 20 consecutive runs. Its simple history fixture uses deliberately
distinct observation timestamps to avoid host clock-resolution ties; it is not
a PostgreSQL ordering or concurrent-probe acceptance test.

Original sources remain intact. Code reconciliation and a published PR are not
whole-worktree or transcript deletion gates. Tests use local HTTP fixtures; no
real provider, account, paid inference or live deployment is accepted by this
review.
