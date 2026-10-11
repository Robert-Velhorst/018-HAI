# Production CI Isolation F1

Date: 2026-09-30
Status: implementation and local contracts verified; hosted execution unverified.

## Owned Files

- `.github/workflows/ci.yml`
- `scripts/test_ci_contract.py`
- `output/ci-isolation-production-20260930.md` (new)

The workflow and contract file were already modified in the shared workspace.
Existing unrelated changes were retained. The launcher belongs to F2 and was
read, not edited. No other project files were changed by this task.

## Browser Job

The job now calls `isolated-acceptance-stack.ps1` with `Prepare -ExecutionMode
manual-local`, then `Start`, then browser acceptance, and finally a separate
`always()`-guarded `Stop`. Stop receives exactly the evidence directory returned
by Prepare, not a search pattern or reconstructed installation path. Prepare
must return one directory directly under the temporary root with the launcher's
unique owner naming format.

Only `HAI_ACCEPTANCE_EVIDENCE`, a nonsecret path, crosses steps through
`GITHUB_ENV`. The browser test process privately reads the manifest and resolved
synthetic configuration, registers secret masks before Start, and sets browser
credentials only in its process environment. The launcher necessarily reads its
own manifest for Start/Stop; no separate workflow credential-export step does.

The browser job has no direct ordinary Compose calls, personal environment file,
fixed installation container name, persistent installation volume, external
account/provider credential, `down -v`, or orphan-removal command. The launcher
supplies synthetic credentials and a disposable owned configuration.

Node 24, npm 10.9.8 installation/version gate, locked `npm ci`, TypeScript
checking, Chromium installation, real browser tests, and readiness checking are
retained. Readiness targets the manifest's loopback port and requires a serving
`ready` or `degraded` response. `/readyz` is an exact gateway location forwarding
to the backend, whose current readiness handler returns 200 for those states.

Reports/results use the unique evidence directory. HTML reports, failure
screenshots and videos remain configured. CI traces are explicitly disabled
because they can retain login request bodies. Artifact paths allow only browser
reports/results and guarded owner logs; they do not upload `manifest.json`,
`compose.json`, `synthetic.env`, raw startup logs, or the evidence root.

Diagnostics select container IDs by the exact owner label and validate every
selected container's owner, Compose project, name and mounts before reading any
logs. Persistent volumes, writable binds and binds outside the evidence directory
are rejected. Saved logs and captured command output redact known synthetic
secrets. Diagnostics cannot mutate containers; cleanup stays with F2's guarded
Stop. Failure in startup, readiness, browsers or diagnostics remains a job failure
and does not suppress the separate cleanup step.

## Verification

- `python scripts/test_ci_contract.py`: **76 tests passed**, no skips in this
  local run. Previous unrelated assertions remain. The workflow's actual Node
  program is executed against mocked OS/process boundaries, including startup,
  readiness, browser and diagnostic failures, bad manifests, foreign ownership,
  persistent volumes, unsafe binds, secret redaction and empty resource sets.
- The embedded readiness program was executed against synthetic responses:
  ready/degraded accepted; non-serving, invalid JSON and unavailable targets fail.
- PowerShell parsed the workflow's Prepare/Stop scripts and the current launcher.
  The actual launcher prefix/TrimEnd expression, explicit slash trimming, empty
  JSON external-property presence and JSON network enumeration passed local
  PowerShell checks. These are syntax/behavior checks on Windows, not Linux runtime
  acceptance. The launcher's GetFullPath/Join-Path/directory-separator code was
  inspected for Linux compatibility without editing it.
- PyYAML parsed all **14 jobs** and **9 browser steps**, rejecting duplicate keys;
  `push` and `pull_request` triggers remain intact.
- In-memory mutation checks rejected replacement of guarded Stop with installation
  `down -v`, and removal of the owner-label log guard.
- `git diff --check` passed for the owned tracked files; only Git's existing
  LF/CRLF conversion warnings were emitted.

## Limitations

No hosted GitHub Actions run, Docker launch/stop, real browser run, provider
request, account access, commit, push or descendant chat was performed by F1.
The parent's reported launcher/Angular runs are not claimed as independent F1
verification. No actual installation resources were accessed or modified.
Hosted Ubuntu launch, Docker ownership/mount behavior, rendered browser reports,
artifact upload and cleanup after real failures still need the authorized CI run.
