# Frontend authentication source reconciliation

## Original source decisions

Reviewed the complete original authentication service, interface and service
test file against the current versions. The original service behavior remains;
its login return type and interface now consistently describe the existing
empty-response contract as `Observable<void>`. Both original service scenarios
and their assertions remain within the ten current Angular HTTP-testing cases.

Retain the newer explicit remember-me preference, registration and local-preview
POSTs, bounded capability/session observations, distinction between HTTP 401 and
unavailable authority, JSON-body reset confirmation and state-changing POST
logout. No implementation is replaced by an older retained copy.

## Historical patch dispositions

Five indexed authentication-service patch variants were inspected:

| Historical contribution | Current disposition |
| --- | --- |
| Remove raw error logging, then turn every session error into false | Keep the logging removal; retain false only for HTTP 401 and propagate other failures. |
| Add typed registration | Keep registration and the newer explicit login preference/empty-response contract. Do not restore historical GET logout from the patch context. |
| Add password recovery with a reset bearer in the URL | Keep recovery and form-encoded email; retain the newer JSON-body reset token and password instead of the URL-bearing variant. |
| Add authentication capabilities | Keep the endpoint with the newer bounded and cancellable observation. |
| Add explicit local preview | Keep the POST; the client alone does not grant preview access or bypass IDP policy. |

These are dispositions of five specific indexed code variants, not an archive
completion claim. Surrounding non-code events, other patches and full archive
integrity/deletion gates remain open.

## Added verification

Five native checks exercise the actual transpiled service with Angular HTTP
value types and RxJS pipelines. They cover request method/body contracts,
special-character email encoding, reset-bearer/password URL exclusion, all six
uncertain mutation failures without resubscription, HTTP 401 versus other
session failures, no raw-error logging and stalled-read cancellation using
virtual time. The existing shared native command imports these checks.

All 276 local native frontend tests pass with no skips or failures. The five
focused service checks are a subset. An initial isolated run exposed a missing
Angular compiler import in the new test harness; that harness was corrected
before the passing runs. No production service change or dependency install
was needed.

This is not live login, cookie persistence, OAuth consent, reset-mail delivery
or deployed account acceptance. No account, permission, provider or credential
was changed. Original sources and transcript archives remain retained pending
their separate preservation and deletion gates.
