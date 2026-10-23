# Gateway source reconciliation

## Retained source decisions

Read the retained gateway template and the full current template and difference.
Preserve per-request Docker resolution, authenticated backend routing, verified
identity forwarding, upload limits, the narrow OAuth callback, internal auth
subrequests and separation of local agent protocols from browser API access.
Keep newer security headers, the frontend listening port, separate rotated
access/refresh cookies, hidden internal response headers, bounded readiness
proxies and long-running operational request timeouts.

The retained generated configuration was inspected with credential-bearing
header values withheld. Its reusable routing is represented by the stronger
current template; it is not a source artifact to copy into Git. Do not restore
its older frontend port, single-cookie refresh behavior or missing host-runtime
gateway exclusion. Generated credentials, personal runtime data and operator
settings are deliberately not published or used to change the running system.

## Publication boundary

The generated `nginx-config/nginx.conf` was not explicitly ignored. A new Git
contract failed before the repair because Git did not classify this path as
ignored. Add an exact root-anchored ignore entry. The contract checks actual Git
ignore behavior, verifies the generated path is not already tracked and ensures
the template remains eligible for publication. This does not untrack an existing
file, remove a credential from history or prove a credential has been rotated.

An additional regression preserves the current backend/IDP readiness deadlines
and the IDP probe's request-body/header suppression. No production gateway
template or active installation configuration is replaced.

## Verification and limits

Run the complete gateway contract suite with the existing Python runtime:

```text
python nginx-config/test_gateway_contract.py
```

All 22 gateway checks and the Docker build-context contract pass. Git path
classification uses binary NUL-delimited stdin/output so Windows text-mode
newline translation cannot create a false ignore result.

These are static source and Git-boundary checks, not live Nginx syntax,
upstream HTTP, actual cookie rotation, OAuth or installed deployment acceptance.
No Docker containers, network exposure, provider configuration or local secrets
are changed. Original recovery files remain retained; source reconciliation,
publication, main preservation and deletion are separate gates.
