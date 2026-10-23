# Retained ngrok source reconciliation

## Current evidence

The historical tunnel files are not lost: complete copies were located in
retained HAI worktrees and recovery sources. They are outside the selected
changed-file ledger and must not be treated as covered by its completion count.
This pass read the four files below completely. Local private receipts bind
the recovered bytes to the earlier full-worktree inventory and current Git
state; the originals remain retained.

| Historical path | Bytes | SHA-256 |
| --- | ---: | --- |
| `scripts/start-ngrok.ps1` | 6452 | `0082b8e5e057ae4d88df6e6ea7ab2f520f445ce80037c0f4f2b2af1b46078472` |
| `deploy/ngrok/start-ngrok.sh` | 2095 | `04a99fd4d9831f2c4616a888061c827186a69d566f34bfbf1074907c3859230a` |
| `deploy/ngrok/ngrok.yml` | 150 | `fe52fa614c99b20e99a66a903a385a06c1ecb652f1b887e1205cc9e2131172c4` |
| `docs/ngrok-cloud-access.md` | 2002 | `0f1454ba90fbfb677404f321ff0ed5764b149392c434c617f64a915185ae2699` |

None of these paths exists in the reviewed integration tree. Parsing the current
local Compose document finds 44 services, no ngrok service and no service with
the `cloud-tunnel` profile. There is therefore no current launcher/profile to
which the historical instructions can truthfully direct an operator. This is
source evidence, not a claim about tunnels or accounts running elsewhere.

## Useful historical intent

The PowerShell launcher checks production mode, disabled local-login bypass,
secure cookies, loopback binding, private A2A, positive rate limiting, generated
application secrets, a reserved HTTPS origin and matching optional Google
callbacks. It includes validate/start/stop paths and attempts Compose ownership
checks before managing the tunnel. The shell entrypoint repeats a subset of
those exposure gates. The agent configuration disables remote management and
automatic update checking, limits its inspection listener to loopback and sets
`inspect_db_size` to `-1`. The guide describes explicit opt-in public access.

These are recoverable design inputs, not proof that a current public deployment
is configured, approved or secure.

## Integration blockers

1. **Missing ownership evidence is accepted.** The PowerShell ownership loop
   appends only nonempty working-directory labels. A container with missing
   ownership labels contributes no directory to validate; malformed JSON is
   rejected, but missing evidence is not. A repaired path must require complete,
   per-container identity evidence before either start or stop.
2. **Checked and acted-on project identity is not explicitly bound.** The script
   checks the project name read from its dotenv input, but the Compose commands
   do not pass that name with `--project-name`. Integration must bind ownership
   checks and mutations to the same resolved project/configuration, rather than
   assuming external CLI state cannot change the target. This is a source-level
   gap; no real Docker mismatch was attempted here.
3. **The shell positive-integer gate accepts all-zero strings other than `0`.**
   Its case pattern rejects literal `0`, empty and nondigits, but permits `00`
   and `000`. The PowerShell gate rejects numeric values below one. Both paths
   need the same bounded positive-value contract before relying on rate limits.
4. **A retained test expects missing launcher functions.** The recovery copy of
   `test-ngrok-ownership.ps1` looks up `Read-BoundedResponseBody`,
   `Invoke-PublicOwnershipProbe` and `Test-PublicEndpointOwnership`; none is
   defined in its recovered `start-ngrok.ps1`. That fixture is not runnable
   evidence for this launcher. Its separate discovery helper was only located,
   not fully audited in this pass.
5. **Canonical deployment wiring is absent.** Reinstating a script alone would
   leave the image/configuration, gateway isolation, authentication and signed
   session behavior unproved. Image provenance, secret handling, public access
   approval and real-account acceptance remain separate gates.

## Disposition

The retained CI source also contains
`test_optional_ngrok_tunnel_is_profiled_and_fail_closed`. The current CI source
omits it. Its assertions cover the optional profile, immutable/unprivileged
service, no host port, default-private A2A, rate limits, launcher/entrypoint gates
and exact Google callback paths. Retain this test with the historical feature:
there is no current replacement to count as integrated. This is separate from
the dependency-audit test, which was renamed and strengthened rather than lost.

Keep the four historical sources and the inconsistent fixture intact. Do not
publish them as a working installer feature, enable public access, or call their
work integrated merely because an audit record exists. A subsequent deliberate
port must repair the blockers, exercise isolated negative and positive cases,
preserve canonical HAI authorization and approval controls, and obtain the
required authority before any live public exposure. Explicit retirement with
verified preservation is an alternative decision, not something this review
silently assumes.

No launcher, tunnel, Docker operation, ngrok account, provider, credential or
installed configuration was used or changed. No original was removed. This
record closes the source-location question, not the integration or deletion
gate for these files.

## Complete helper-source follow-up, 24 October

The retained preparation, discovery and ownership-test helpers were subsequently
read completely together with the launcher, rather than inferred from their
filenames or function names. Their inspected source hashes are:

| Historical path | Bytes | SHA-256 |
| --- | ---: | --- |
| `scripts/prepare-ngrok-windows.ps1` | 4556 | `8ee02f5d8a2634f37562302cef87b2d31f61275fc7c64b594672453354827dea` |
| `scripts/discover-ngrok-windows.ps1` | 6135 | `248832e52da11850e2c2724e22847c468cd374a3b70f28f53e0bc48fa8a85a3d` |
| `scripts/test-ngrok-ownership.ps1` | 4992 | `37362b6ff7a695ee9c5cf823918394d07c2022220a71413305cbd62f342168b1` |

Additional integration issues remain:

- Preparation exposes `-PublishA2A`, sets the public flag and rewrites the A2A
  endpoint, whereas the retained launcher requires that flag to be false and
  the canonical bridge requires a private endpoint. Do not copy that switch as
  an operational capability or silently weaken the bridge boundary.
- The ownership-test scenarios accept a generic `service=backend` response when
  a caller-supplied boolean says the current tunnel is running. That marker
  does not bind a remote response to this installation, project or authenticated
  owner. Even implementing the missing functions would not by itself make this
  a sufficient endpoint-ownership acceptance test.
- The discovery helper starts a real agent against loopback port 1 to discover
  an account endpoint. Reading it is not authorization to launch that agent or
  contact an account. It also reads each entire supplied diagnostic log before
  truncating the final summary; a port should bound input bytes, not only output.
- Preparation treats external values as regular-expression replacement text
  when editing dotenv declarations and does not establish a single-declaration
  or single-line value contract. Use literal structured edits, reject ambiguous
  declarations and newline/control-character values, and preserve the canonical
  protected-environment and fail-closed initialization behavior. No real token
  or installed environment was read to establish this source-level concern.

The canonical Windows installer already has tested project/working-directory/
Compose-file ownership inspection and explicit project selection. Evaluate reuse
before introducing another independent ownership implementation; do not weaken
that existing path to accommodate the older launcher. The source review above
does not execute any helper, create a public endpoint, recover a working profile
or close the integration/deletion gate.
