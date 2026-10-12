# Governed ngrok cloud access

HAI is local-first. The `cloud-tunnel` Compose profile is deliberately off by
default and is the only supported route for temporary public HTTPS access to a
loopback-bound local HAI installation.

Before enabling it, create `.env.local` from `.env.example`, use real generated
secrets, reserve an HTTPS ngrok domain, and configure a dedicated ngrok token.
The tunnel refuses to start unless production mode, secure cookies, loopback
gateway binding, disabled public A2A exposure, and a positive
`RATE_LIMIT_PER_MINUTE` are all explicit. The default rate limit remains `0`
for local-only development; set an appropriate positive value before exposing
HAI publicly.

Validate the configuration without creating a tunnel:

```powershell
.\scripts\start-ngrok.ps1 -ValidateOnly
```

Start or stop the tunnel only from the checkout that owns the `018-hai`
containers:

```powershell
.\scripts\start-ngrok.ps1
.\scripts\start-ngrok.ps1 -Stop
```

The launcher rejects placeholder secrets, another checkout's containers, an
unreserved URL, insecure cookies, non-loopback gateway binding, and any attempt
to advertise the local-only A2A bridge. Public access does not bypass normal
application authentication or approval rules.

Google is optional. When it is enabled for a public installation, register both
of these exact callback URIs on the Google OAuth web client and put the same
values in `.env.local`:

```text
<HAI_NGROK_URL>/api/v1/auth/google/callback
<HAI_NGROK_URL>/api/v1/sources/oauth/google/callback
```

The tunnel refuses a configured Google callback that still points at localhost
or a different origin. Leave a callback setting empty to keep that optional
Google flow disabled.

The local A2A Agent Card and planning endpoint use a separate `local-a2a`
Compose profile bound to `127.0.0.1:8091`. They are not routed by the public
dashboard gateway, so enabling this tunnel cannot publish that local connector.
