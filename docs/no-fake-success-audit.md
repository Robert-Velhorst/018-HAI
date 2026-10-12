# No Fake Success / No Mock Production Behavior — Audit

Verifies the repository upholds the core rule: no mockups, no fake integrations,
no false completion, and no mock behavior masquerading as production.

## Checks

| Check | Result |
| --- | --- |
| Demo/test modes clearly labelled and side-effect free | Yes — `internal/demomode` (production fail-safe; demo/test carry `[DEMO]`/`[TEST]` labels; only production allows real side effects). |
| Fake providers isolated to tests | Yes — `internal/fakeprovider` is test-only and never wired into production routing. |
| Connector states are truthful | Yes — capability registry marks connectors implemented/stub/disabled/blocked (register #44). |
| Paid usage cannot silently happen | Yes — paid budget €0; policy test proves paid usage impossible at that setting. |
| Status claims are evidence-backed | Yes — completion matrix rows cite concrete code/tests; roll-up recomputed to match reality. |
| Manual steps not pretended automatic | Yes — assisted workflows tell the user what remains manual. |
| Run mode visible | Yes — `/system/info` + `runtime.mode` doctor check expose production vs demo. |

## Run

Use the native audit for the platform running the checkout:

```powershell
.\scripts\test-no-fake-claims-audit.ps1
```

```sh
scripts/no-fake-claims-audit.sh
```

Both audits are static safeguards. They catch repository claims, unsafe tracked
artifacts, and required fail-closed code guards; they do not establish that an
external account, model provider, or runtime has completed a live task.

## Anti-patterns searched for

- Endpoints returning hard-coded "success" without doing work → none found in new
  code.
- UI actions with no backend → none found (UI action audit).
- Docs claiming shipped behavior without code/test → corrected where found
  (documentation-truthfulness audit).

## Verdict

The no-fake-success rule holds. Demo/test behavior is labelled and cannot be
confused with production; disabled integrations are shown as disabled, not faked;
and completion status is evidence-based with an explicit tested-vs-wired
distinction.
