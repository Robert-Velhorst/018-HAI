# Connected Sources Action-Chain Audit

Date: 2026-09-30. Checkout: `codex/hai-runtime-release` in the shared dirty worktree.

## Scope and Safety

This audit changed only Connected Sources page files and its dedicated service/interface/tests. Existing unrelated changes and diagnostics were retained. No commits, pushes, deployments, installation restarts, provider calls, account actions, database mutations, Docker containers, or cleanup operations were performed. Parallel-agent tooling was unavailable in this session; no descendant agents were spawned.

Backend contracts were inspected directly, read-only, in `backend/internal/router/routes.go`, `backend/internal/source/handler.go`, `backend/internal/source/service.go`, `backend/internal/source/manual_sync.go`, and `backend/internal/source/authorization.go`. Runtime dependencies were discovered with the desktop dependency tool. All executed HTTP assertions used Angular's synthetic HTTP testing controller; all component actions used synthetic service fixtures. These results are not live-provider or deployment acceptance.

## Concrete Findings and Changes

| Finding | Scoped fix |
| --- | --- |
| Revoke and Delete sent no durable approval references, making their buttons fail against the actual backend contract. | Advanced review dialog requires existing task, approval source, binding digest, and idempotency references. Service sends all four `X-HAI-*` headers. No authority is generated or bypassed by the client. |
| A malformed or wrong-source sync acknowledgement could be treated as accepted and its retry key discarded. | Validate acknowledgement identity, mode, status, timestamps, and bounded integer counters before acceptance. Keep the same retry key on an uncertain response. |
| Polling could accept another source/job's status. | Validate both source and job IDs; retain queued/running state and provide read-only status recovery on bounded polling failure. |
| Cached failed/running manual jobs could mask newer authoritative jobs or refreshed terminal history. | Choose newer job evidence and allow terminal history for the same job to supersede a stale in-flight poll. Status recovery follows the currently displayed job. |
| Local-only and modeled adapters, or mere active registration, could be reported as live connections. | Distinguish local-only/modeling capability from remote connections; active registration is configured, not live verification. Unknown adapter capability fails closed. |
| Source-specific configuration failures and unavailable health were not blocking non-Google sync controls. | Gate those states and expose health-check retry for all connector types. |
| Google source creation allowed duplicate in-flight calls; paused/mutating sources could begin consent. | Guard duplicate creation/authorization and paused/revoked/mutating source state. |
| Basic lacked pause/resume; Advanced lacked the Basic sync recovery actions. | Wire both views to the existing pause/resume, safe retry, and status endpoints. Pause explicitly does not promise current-job cancellation. |
| Failed source mutations were toast-only, and destructive recovery lacked a direct history action. | Retain a sanitized operation error and open the existing Advanced activity disclosure for review without resubmitting. |
| Missing `X-Total-Count` became numeric zero despite returned records. | Fall back to returned page length while retaining real backend totals/limits. |

## Endpoint Contract

All paths below are relative to `/api/v1/sources`:

- Catalog/source/health: `GET /connectors`, `GET /?includeDisabled=...`, `GET /connection-health`, `GET /:id/health`.
- Registration and consent: `POST /`, `GET /oauth/google/start?sourceId=...`.
- Durable manual sync: `POST /:id/sync-jobs` with `Idempotency-Key`; `GET /sync-jobs/:id` with no-cache status reads. Queue acceptance is not completion.
- Explicit local runners: `POST /:id/transcribe`, `POST /:id/extract-documents`. No provider or microphone authority is added.
- Existing import/re-index/due operations remain at `POST /:id/sync`, `POST /:id/reindex`, `POST /sync-due`.
- Pause/resume: `POST /:id/pause`, `POST /:id/resume`. Revoked sources are not reactivated.
- Approval-gated destruction: `POST /:id/revoke`, `DELETE /extractions/:id`; both require `X-HAI-Task-ID`, `X-HAI-Approval-Source-ID`, `X-HAI-Approval-Binding-Digest`, and `X-HAI-Idempotency-Key`.
- Existing correction revision/idempotency recovery and advisory-only pursuit/life-graph semantics were preserved. No seven-tier or other-module semantics were changed.

## Exact Focused Runner

Working directory:

```text
C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk\frontend
```

PowerShell command executed with bundled Node **v24.19.0**:

```powershell
$env:CHROME_BIN='C:\Program Files\Google\Chrome\Application\chrome.exe'
$env:NG_CLI_ANALYTICS='false'
& 'C:\Users\NO\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe' node_modules/@angular/cli/bin/ng.js test --watch=false --browsers=ChromeHeadless --ts-config=src/app/pages/connected-sources/tsconfig.action-chain.spec.json --include=src/app/pages/connected-sources/connected-sources.component.spec.ts --include=src/app/services/connected-source/connected-source.service.spec.ts --progress=false 2>&1 | Tee-Object -FilePath "$env:TEMP\hai-connected-sources-action-chain-20260930-final.log"
exit $LASTEXITCODE
```

Final result: **111 SUCCESS**, exit code 0, Chrome Headless 154 on Windows. Bundle compilation completed in 4.907 seconds; tests completed in 1.442 seconds. Final bundle timestamp: `2026-09-30T14:32:17.362Z`.

Evidence log: `C:\Users\NO\AppData\Local\Temp\hai-connected-sources-action-chain-20260930-final.log`.

The suite includes a rendered native-button Basic Pause -> Advanced danger disclosure -> review dialog -> explicit four-field submission -> emergency-stop error flow. The service tests assert exact routes, methods, headers, owner-scoped status recovery, and extraction count fallbacks. No real account is used.

Earlier runner results are retained honestly: the original focused configuration failed compilation because it did not include the service spec; an intermediate pre-existing-test run had 87 successes and 3 failures after tightening state semantics. Those three fixtures were corrected to explicitly represent enabled Google sources and local-only rather than live connection status. The expanded run passed 109 tests before the final two recovery regressions were added; its log remains at `%TEMP%\hai-connected-sources-action-chain-20260930-run2.log`.

Additional commands:

```powershell
& 'C:\Users\NO\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe' scripts/check-progressive-sections.mjs
git diff --check -- frontend/src/app/pages/connected-sources frontend/src/app/services/connected-source frontend/src/app/services/connected-source.service.interface.ts
```

Progressive-section check: exit 0, 29 module templates, 146 static section IDs, 2 dynamic prefixes. Scoped whitespace check: exit 0; Git reported existing Windows LF/CRLF conversion warnings, not whitespace errors.

## Integration Limits

- There is no source-specific approval-preparation endpoint in the inspected source routes. The review dialog accepts existing durable references only; it neither creates a review decision nor proves that an approval-capable operator can obtain a matching reference end to end. Approval issuance/acceptance remains a backend/operator integration dependency.
- There is no per-job source cancellation endpoint. Pause stops future sync admission; current work may finish. Revoke is approval-gated and is not presented as immediate cancellation.
- Google `ready` health describes an encrypted read-only grant, not a fresh provider call. Trello operational health describes recent complete sync evidence, not continuous availability. Backend reason text is retained rather than inventing live evidence.
- No production build, real-provider OAuth/sync/revocation/deletion, deployed screenshot, or mobile visual acceptance was run. Parent owns the fresh isolated production-mode stack at `http://127.0.0.1:58143`, its rebuilt frontend snapshot, synthetic-fixture stack acceptance, and the global integration suite. This audit did not operate that stack.
- Uncertain destructive responses block resubmission for that resource/action in the current page instance. Inputs are not persisted and no automatic replay occurs across reloads; backend approval consumption and exact-effect checks remain authoritative.

## Files Changed by This Audit

- `frontend/src/app/pages/connected-sources/connected-sources.component.ts`
- `frontend/src/app/pages/connected-sources/connected-sources.component.html`
- `frontend/src/app/pages/connected-sources/connected-sources.component.scss`
- `frontend/src/app/pages/connected-sources/connected-sources.module.ts`
- `frontend/src/app/pages/connected-sources/connected-sources.component.spec.ts`
- `frontend/src/app/pages/connected-sources/tsconfig.action-chain.spec.json` (new)
- `frontend/src/app/pages/connected-sources/ACTION_CHAIN_AUDIT.md` (new)
- `frontend/src/app/services/connected-source.service.interface.ts`
- `frontend/src/app/services/connected-source/connected-source.service.ts`
- `frontend/src/app/services/connected-source/connected-source.service.spec.ts` (already untracked on entry; extended, not replaced)

The pre-existing `tsconfig.connected-sources.spec.json` was retained unchanged. Shared shell/styles/registry/auth, backend, and other module files were not edited. Shared-worktree aggregate diff statistics include pre-existing edits and must not be attributed solely to this audit.
