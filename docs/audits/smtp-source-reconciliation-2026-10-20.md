# SMTP retained-source reconciliation - 2026-10-20

Baseline: main `7f93a2229b6cfeb42a9ec1f18f1ae50a3b804017`.

A retained SMTP sender variant normalized interrupted network errors to the
caller's context error. Current main already interrupts sockets on cancellation,
but did not preserve `context.Canceled` or `context.DeadlineExceeded` for callers
using `errors.Is` after the connection opened.

## Reproduction and recovery

`TestSMTPGreetingInterruptionPreservesContextCause` opens a loopback TCP listener
and withholds its SMTP greeting. Both cancellation and deadline cases failed
against baseline: the error chain contained only a closed-socket error or an I/O
timeout. No external SMTP account, credentials, or real message is involved.

The recovered behavior normalizes failed delivery results only after recipient
validation. It preserves the redacted delivery-stage wrapper and joins the
context cause with the original failure. A socket deadline may fire before the
context timer callback; an expired caller deadline plus a network timeout is
therefore recognized as `context.DeadlineExceeded` too.

Successful delivery is not changed into failure merely because the context is
cancelled after SMTP accepted the message. TLS requirements, implicit TLS support,
exact password bytes, envelope/header handling, and public redaction remain as
implemented on main. The older sender is not copied wholesale: that would discard
these newer safeguards.

## Other reviewed declaration differences

- Audit events use shared `safety.IsSensitiveKey` rather than the old local helper;
  serialized literal events also receive redaction.
- Gmail uses shared `ProviderAPIError` in place of the older Gmail-specific error.
- MCP preflight uses `validateEndpointURL` in place of the older local URL helper.
  Local-only defaults remain; remote access requires explicit configuration,
  HTTPS, and a bearer token.

These declaration differences are not missing features. This bounded review does
not establish equivalence of every file in the retained worktrees and does not
authorize deletion of any checkout or transcript.

## Validation

The full IDP services package tests passed after the fix. The two new loopback
cases failed before the production change. Hosted CI, real SMTP-provider delivery,
and live password-reset acceptance remain separate gates.
