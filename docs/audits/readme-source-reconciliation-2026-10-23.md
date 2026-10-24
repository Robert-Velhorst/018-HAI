# README source reconciliation

Read the complete retained README and its complete integration delta. Preserve
the canonical Go/Angular/PostgreSQL product boundary, owner-scoped governance,
approval and verification separation, runtime restrictions, safe source intake
and independently required provider/target-machine acceptance. Do not restore
obsolete toolchain versions, retired images or missing ngrok launch instructions.
The historical ngrok feature remains retained and explicitly unintegrated in
its separate reconciliation record, not silently retired.

The current README contains extensive dated continuation evidence. Reading those
records does not reproduce their tests or certify every historical assertion
against current code. Keep their chronology and limitations; add a short reader
index and an explicit distinction between historical evidence, current service
health, source review, PR publication, preservation on main and local deletion.
No percentage-complete or overall production-readiness claim follows from this
review.

## Source-backed corrections

- The auth/SMTP recreation example named `gateway`, a container name, rather
  than the declared Compose service `nginx`. Use the service name.
- Gateway curl examples omitted the default published port 8088. Match the
  environment template and retain unauthenticated readiness and protected-route
  checks; no session token or access bypass is added.
- Replace instructions to edit an obsolete `80:80` mapping with the supported
  `GATEWAY_HOST_PORT` setting, deliberate gateway recreation, loopback binding
  and consistent browser/probe/OAuth callback URLs.
- The blanket missing-provider-webhook statement ignored the implemented signed
  Trello receiver. Link its setup/acceptance contract without claiming that a
  callback is publicly reachable, registered or live-verified.
- The blanket missing-heartbeat statement contradicted the durable runner's
  lease-renewal loop. Distinguish that implementation from accepted distributed
  or high-availability operation.
- The operations table implied unconditional source/workflow ticker fallback.
  Current source startup requires `SOURCE_SCHEDULER_VOLATILE_FALLBACK`; workflow
  startup uses `DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED`. Both example defaults
  are false. Do not enable either as part of documentation repair.
- Google source setup refers to each intended API rather than an unexplained
  pair, keeping identity login and data-source callbacks separate.

## Verification limits

Two new source-contract tests fail before the command corrections: the auth
example targets an undeclared service and all four probe URLs omit the configured
port. After correction all 127 CI contract tests pass locally without skips.
The checks parse documentation and checked-in configuration; they do not execute
the documented commands, start Docker, send mail or perform Google consent.

All 97 relative Markdown file links resolve locally after the additions; seven
local section anchors match the README's heading slugs. Link
resolution is not semantic verification of every linked document. Historical
test totals were not updated or reused as current test results. No original,
archive, installation, account, credential or running application was changed.
