# Governed ngrok Cloud Access

HAI remains loopback-only by default. Public access is an explicit Compose
profile and must be started through the Windows preflight script. The tunnel
forwards only to the nginx gateway on the private `service-hub` network; it does
not publish Postgres, Redis, Kafka, the backend, the IDP, or ngrok's inspector.

## Security boundary

The preflight fails closed unless:

- `RUN_MODE=production`;
- `LOCAL_LOGIN_BYPASS_ENABLED=false`;
- `IDP_COOKIE_SECURE=true`;
- `GATEWAY_HOST_BIND=127.0.0.1`;
- `RATE_LIMIT_PER_MINUTE` is a positive integer;
- the ngrok token and HAI signing/encryption secrets are non-placeholder values;
- `HAI_NGROK_URL` is a fixed HTTPS origin; and
- configured Google login/source callbacks exactly match that public origin.

When the bounded A2A planning connector is publicly enabled, preflight also
requires the base bridge to be enabled, a separate 32-or-more-character bridge
token, one named owner, and an endpoint exactly equal to
`${HAI_NGROK_URL}/api/v1/a2a`. The container entrypoint independently repeats
these connector checks, so invoking Compose directly cannot create a tunnel
whose advertised A2A endpoint disagrees with the public origin.

The ngrok edge applies one of two immutable traffic policies. Both add an HSTS
response policy. Unless public A2A is explicitly enabled, the private policy
returns HTTP 404 for both the Agent Card and `SendMessage` path at the edge,
while the same connector can remain available through the loopback gateway.
This means opening the general HAI tunnel cannot silently publish a locally
enabled connector.

The container entrypoint independently rechecks production mode, disabled
local-login bypass, secure cookies, loopback gateway binding, enabled API rate
limiting, a dedicated token, and a fixed ngrok HTTPS origin. Calling Compose
directly therefore cannot bypass the core exposure gate; the PowerShell
preflight adds the stronger secret and OAuth consistency checks. The gateway
also applies a small fixed-memory throttle to unauthenticated authentication
and A2A routes, independently of the backend's Redis-backed limiter.

Use a dedicated ngrok authtoken with an ACL restricted to the reserved HAI
domain. Do not reuse a general account token. The agent image is digest-pinned,
remote management and request inspection are disabled, and the container is
read-only with dropped capabilities and bounded CPU, memory, and process count.

## Start on Windows 11

If the Windows ngrok profile contains an authtoken but its assigned endpoint is
unknown, discover the endpoint without forwarding HAI itself:

```powershell
$publicUrl = .\scripts\discover-ngrok-windows.ps1 -InspectionPort 4045
```

The discovery helper uses a separate local inspection port and an unused
upstream, validates that ngrok returned an HTTPS ngrok hostname, stops only the
process it created, and removes its temporary files. It refuses an occupied
inspection port and leaves every existing ngrok process untouched. If ngrok
reports `ERR_NGROK_334`, that account endpoint already belongs to a running
agent. Do not enable pooling: stop the owning application during an approved
maintenance window or reserve a separate fixed HAI domain.

Generate a separate hardened cloud profile instead of weakening the local
profile. The command disables local-login bypass, enables secure cookies, keeps
the gateway loopback-bound, applies a positive rate limit, sets exact OAuth
callbacks, and validates the result before retaining it:

```powershell
.\scripts\prepare-ngrok-windows.ps1 `
  -PublicUrl $publicUrl `
  -PublishA2A
.\scripts\start-ngrok.ps1 -EnvFile .env.ngrok.local
```

Omit `-PublishA2A` to keep the connector private while exposing only the normal
authenticated dashboard. `.env.local` remains unchanged.

Set these values in the uncommitted `.env.local` file:

```text
NGROK_AUTHTOKEN=<dedicated ACL-restricted token>
HAI_NGROK_URL=https://your-hai-domain.ngrok.app
RUN_MODE=production
LOCAL_LOGIN_BYPASS_ENABLED=false
IDP_COOKIE_SECURE=true
GATEWAY_HOST_BIND=127.0.0.1
RATE_LIMIT_PER_MINUTE=120
```

Optional bounded A2A planning through this same origin requires:

```text
HAI_A2A_BRIDGE_ENABLED=true
HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=true
HAI_A2A_BRIDGE_OWNER_ID=<one configured HAI owner identity>
HAI_A2A_BRIDGE_TOKEN=<dedicated random token of at least 32 characters>
HAI_A2A_BRIDGE_URL=https://your-hai-domain.ngrok.app/api/v1/a2a
```

This exposes an authenticated planning draft only. It does not create or
execute tasks, disclose HAI source or memory context, approve work, call models
or tools, discover peers, or implement the full A2A task lifecycle.

If Google login or Google connected sources are configured, register and set:

```text
GOOGLE_LOGIN_REDIRECT_URL=https://your-hai-domain.ngrok.app/api/v1/auth/google/callback
GOOGLE_OAUTH_REDIRECT_URL=https://your-hai-domain.ngrok.app/api/v1/sources/oauth/google/callback
```

Validate without opening a tunnel:

```powershell
.\scripts\start-ngrok.ps1 -ValidateOnly
```

Start and stop the profile:

```powershell
.\scripts\start-ngrok.ps1
.\scripts\start-ngrok.ps1 -Stop
```

Startup does not declare the endpoint available merely because the tunnel
process is healthy. It waits for the real fixed HTTPS origin, checks backend
health, aggregate readiness, an anonymous no-permission session, the Angular
shell, and HSTS. The local and public readiness checks reject a payload that
contains internal dependency details or is cacheable. The launcher also proves
that A2A is edge-blocked when public mode is disabled, or runs the authenticated
bounded A2A acceptance when public mode is enabled. A failed public-origin probe
stops the tunnel instead of leaving an unverified endpoint online.

Before changing any running HAI service, the launcher performs a bounded,
non-invasive ownership probe against the fixed endpoint. If another
application already responds there, or another HAI tunnel owns it outside the
current Compose project, startup stops before reconciliation and explains that
a separate domain or approved maintenance window is required. The probe reads
at most 8 KiB from an untrusted response and never stops or reconfigures the
owning agent. An unreachable or offline ngrok endpoint is not treated as an
error because the full post-start acceptance remains authoritative.

Verify the A2A chain locally before exposure, then through the live tunnel:

```powershell
.\scripts\smoke-a2a-bridge.ps1
.\scripts\smoke-a2a-bridge.ps1 -Public
```

The public smoke requires the real tunnel and its configured token. It checks
the Agent Card, denial without that token, and a successful non-executable
planning response without printing the credential.

Build the ordinary HAI stack before the first tunnel start. On every start, the
script reconciles and health-checks the IDP, backend, frontend, and gateway with
the hardened environment before it creates the public endpoint. This prevents
an IDP that was previously started with insecure cookie settings from being
exposed merely because the file was edited afterward. A healthy container and
reachable login page prove only transport and authentication routing. Each
external connector, model, or runtime still requires its own bounded
authorization and acceptance evidence.

## Recovery

If the tunnel is unhealthy, inspect only the bounded service log:

```powershell
docker compose --env-file .env.local --profile cloud-tunnel -f docker-compose.local.yml ps ngrok
docker compose --env-file .env.local --profile cloud-tunnel -f docker-compose.local.yml logs --tail 100 ngrok
```

Stopping the tunnel does not stop HAI. Rotating the dedicated ngrok authtoken
does not change HAI sessions, but existing public sessions should be revoked if
the endpoint or token was exposed.
