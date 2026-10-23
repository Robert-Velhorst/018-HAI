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
