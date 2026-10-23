# Gateway source reconciliation and header inheritance

## Source decisions

Reviewed the complete original gateway template, gateway contract tests and
config-manager service tests against the integration versions. Retain the
current port-8080 frontend upstream, approval-upload envelope, security
headers, bounded readiness requests and authenticated route/bridge boundaries.
The eighteen original gateway test scenarios remain represented, with the
generated-config scenario intentionally checking the canonical template
instead. Current coverage also checks that a secret-bearing generated config
is ignored and not tracked. The six original config-manager service test
functions remain, adapted to the newer changed/error return contract and
supplemented by atomic replacement and replay checks.

The original generated `nginx.conf` contains literal credential directives.
Its contents are not published or printed. It remains a private generated
artifact, not an alternative source template to restore into Git. Classifying
that artifact is not a complete semantic review of every private setting and
does not authorize deleting it or its supporting environment files.

## Corrected behavior

The compatibility route and generated automation route each set rotated-cookie
headers. Under Nginx's default rules, those local `add_header` directives
prevent inheritance of the parent security headers. The gateway template
already repeats policy on its explicit API locations, but these two dependent
route sources lacked equivalent protection.

Both now use `add_header_inherit merge;`. This keeps parent security headers
alongside the two cookie headers without copying the policy into another
template. It requires Nginx 1.29.3 or later; the repository's CI gateway image
is pinned to 1.30.6. See the official
[header-inheritance documentation](https://nginx.org/en/docs/http/ngx_http_headers_module.html#add_header_inherit).
No live config reload, gateway restart, network exposure or credential change
was performed.

## Verification

The new regression failed on both route sources before the correction. All
24 gateway contract tests then passed. A bounded native Go test also rendered
the actual config-manager template and verified authentication, both cookie
headers, upstream routing and the inheritance directive. This was a
standard-library-only target, not the complete config-manager package or a
live Nginx response test. New-head CI syntax/runtime acceptance remains needed.

Separately, published head `3056fa0e3d22c34d764eaa4d01ec4a52850ae279` now has
successful backend, IDP and frontend CI jobs. The IDP's guarded password-reset
and account-mutation PostgreSQL step and durable PostgreSQL/Redis session step
both succeeded. This verifies the earlier fixture corrections on that head;
it does not make the whole workflow green or validate subsequent local changes.
The known shared-command contract correction remains in a local follow-up,
and the Promptfoo dependency audit remains a separate release gate.

Original source copies, private generated configuration, other clones and
transcript archives remain retained. No main-preservation or whole-scope
completion claim follows from these file reviews and tests.
