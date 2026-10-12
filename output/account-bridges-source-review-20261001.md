# Account Bridges Source Review

Parent integration amendment, 2026-10-01: All five findings below were addressed
in the shared checkout. Unknown totals are no longer zero, retained evidence stays
explicit, loading/failed-refresh guards cover Basic/Advanced and methods, accessible
names contain visible labels, and timestamps render separate UTC times and valid
datetime attributes. DOM tests cover binding, failure retention and recovery.
The final focused Angular AOT/template/native Chrome run passes 30 component tests;
see `production-low-resource-account-bridges-reviewed-final-20261001.receipt.json`.
The original source-only review, fingerprints and line references below are retained
as historical audit evidence, not current post-fix references. No deployment,
whole-app visual/responsive or assistive-technology acceptance is implied.

Date: 2026-10-01. Scope: timestamp UI, regression tests, accessibility, and nearby template/runtime risks in the current dirty working tree.

## Findings

### 1. [P2] Failed initial feed loading is still presented as a known empty registry

Sources: [template lines 45-60](../frontend/src/app/pages/account-bridges/account-bridges.component.html):45, [next-action detail lines 98-105](../frontend/src/app/pages/account-bridges/account-bridges.component.ts):98, [error-state test lines 377-385](../frontend/src/app/pages/account-bridges/account-bridges.component.spec.ts):377.

When the first feed request fails, `feeds` remains empty and `loading` becomes false. All four Basic metrics therefore show numeric zero, and `nextActionDetail` says "No normalized account feeds are registered." The live status correctly says "Feed status unavailable," but the metrics and next-action explanation contradict it. The existing error test only excludes the Advanced empty-state text, so it misses this Basic-state defect.

Recommendation: distinguish never-loaded/unavailable feed state from a successful empty result. Show unavailable metrics and a status-recovery explanation; retain explicitly last-known metrics only after a prior successful load. Extend the failed-initial-load test to assert the Basic metrics and explanation.

### 2. [P2] Advanced per-feed actions bypass the refresh loading guard

Sources: [Basic loading guard at template line 74](../frontend/src/app/pages/account-bridges/account-bridges.component.html):74, [Advanced action lines 149-157](../frontend/src/app/pages/account-bridges/account-bridges.component.html):149, [refresh retention lines 125-158](../frontend/src/app/pages/account-bridges/account-bridges.component.ts):125, [sync guards at lines 177-179 and 206-207](../frontend/src/app/pages/account-bridges/account-bridges.component.ts):177.

After a successful load, start a pending refresh with Registered feeds expanded. The old feed objects remain visible; their "Sync now" buttons do not include `loading` in the disabled expression, and `sync()` does not reject loading either. A user can submit an import based on the old connection state before the new state arrives, despite the Basic action and bulk button being disabled pending refresh. This is an inconsistent UI guard, not evidence that backend authorization can be bypassed. `syncAll()` also omits the loading check enforced by its own button.

Recommendation: include the refresh guard in the Advanced buttons and both action methods. Add a pending-refresh test on an already populated fixture asserting disabled actions and no sync request.

### 3. [P2] Primary action's accessible name does not contain its visible label

Sources: [template lines 70-79](../frontend/src/app/pages/account-bridges/account-bridges.component.html):70, [Advanced action lines 156-159](../frontend/src/app/pages/account-bridges/account-bridges.component.html):156.

With an available feed, the button visibly says "Sync this feed" while `aria-label` replaces its name with "Sync Calendar feed now" (or another feed name). Without an available feed, "Review sources" becomes "Review connected sources." Neither accessible name contains the complete corresponding visible phrase. Advanced per-feed buttons similarly separate the visible words "Sync now" with the feed name in their accessible labels. These are source-visible label mismatches that risk speech-control users being unable to activate the phrases they see.

Recommendation: use names such as "Sync this feed: Calendar feed," "Review sources: connected sources," and "Sync now: Calendar feed," or preserve visible text and add feed context through accessible description. Assert visible-label inclusion for both Basic states and Advanced buttons. Actual speech-control and accessibility-tree behavior remains unverified.

### 4. [P2] Timestamp tests do not protect the timestamp template bindings

Sources: [helper-only cases at spec lines 223-256](../frontend/src/app/pages/account-bridges/account-bridges.component.spec.ts):223, [display bindings at template lines 142-145](../frontend/src/app/pages/account-bridges/account-bridges.component.html):142.

The new failed-first-attempt, failed-after-success, and malformed-date cases invoke component helpers directly. They never mount those feed values, open Registered feeds, and inspect the associated `dt`/`dd` pairs. Changing the "Last successful sync" binding to `lastAttemptAt(f)`, swapping the two bindings, or removing that row would leave those helper assertions unchanged. This is a regression-coverage finding; the current three bindings are correctly separated in source.

Recommendation: add DOM assertions scoped to the relevant feed article and definition term for each timestamp state, including the exact zero read count, no recorded success, invalid latest attempt, and legacy success-only fallback. Exercise a post-sync refresh and ensure a later failed attempt changes the attempt row without advancing the successful-sync row.

Related retention coverage gap: [spec lines 156-175](../frontend/src/app/pages/account-bridges/account-bridges.component.spec.ts):156 claims to test retention but destroys the populated fixture and asserts an empty new fixture. [Spec lines 303-313](../frontend/src/app/pages/account-bridges/account-bridges.component.spec.ts):303 fails the bridges request, not the feeds request. Neither verifies retained feed timestamps/read counts or unchanged `lastLoadedAt` after a failed feed refresh. Add that scenario without remounting the fixture.

### 5. [P3] Displayed timestamp values omit their time zone

Sources: [formatter lines 291-307](../frontend/src/app/pages/account-bridges/account-bridges.component.ts):291, [timestamp rows at template lines 143-144](../frontend/src/app/pages/account-bridges/account-bridges.component.html):143, [formatter expectations at spec lines 238-247](../frontend/src/app/pages/account-bridges/account-bridges.component.spec.ts):238.

Both helpers call `toLocaleString()` without formatting options or a zone label. In common browser locales this displays local wall-clock time without its offset. During a repeated daylight-saving hour, two different instants can have indistinguishable displayed values; copied status text also lacks the zone needed to compare it with provider or server records. The tests compute expected output with the same default formatter, so they do not protect against this ambiguity.

Recommendation: use a shared explicit formatter that includes a zone/offset and expose the original instant using a `time` element with `datetime` where valid. Add deterministic format coverage for distinct instants in a repeated local hour. No current user-visible formatting failure was reproduced in this review.

## Template And Contract Assessment

- No definite missing helper, missing template dependency, or timestamp-related compilation blocker was identified by inspection. This is not a compilation result.
- [AccountBridgesModule lines 16-24](../frontend/src/app/pages/account-bridges/account-bridges.module.ts):16 imports CommonModule, ControlRoomModule, and the button/tag modules used here. CommonModule supplies the DatePipe used for `lastLoadedAt`; ControlRoomModule exports the progressive-section component.
- The installed button declaration accepts widened loading/disabled inputs through `ngAcceptInputType_nzLoading` and `ngAcceptInputType_disabled` at `frontend/node_modules/ng-zorro-antd/types/ng-zorro-antd-button.d.ts:50-52`. Do not report the indexed loading expression at template line 75 as a proven strict-template type failure from its declared boolean property alone.
- Timestamp semantics agree with the current backend source: [health construction at registry_service.go line 173](../backend/internal/accountfeed/registry_service.go):173 uses separate success and attempt maps, and [recordSync lines 362-370](../backend/internal/accountfeed/registry_service.go):362 advances success only for `synced`. Failed and partial attempts therefore do not inherently advance the UI's successful-sync timestamp. This was a supporting source read, not a backend review or live integration check.
- Optional contract-list helpers guard null arrays. They do not validate array members or nested feed objects. Malformed array entries could still cause template/getter exceptions, but the inspected producer emits structured entries; this is a conditional hardening risk, not a demonstrated normal-response bug.
- Likewise, `Number(feed.lastItemsRead)` at component line 300 would convert a runtime null/blank value to zero. The inspected backend emits an integer field, so this is a malformed-response hardening concern rather than a confirmed producer/UI contract mismatch.

## Evidence And Limits

Capabilities used: local numbered source reads, read-only working-tree inspection, the relevant model/service/module and installed dependency declarations, and SHA-256 source fingerprinting. A lightweight memory lookup supplied checkout context only; historical test results were not used as current evidence. The available multi-agent review workflow was not applied because this request explicitly forbids subagents. Browser, provider, deployment, and external connector capabilities were not applicable to this source-only scope.

No tests, builds, browser/UI checks, Docker operations, downloads, subagents, commits, or pushes were performed. No rendered acceptance, screen-reader acceptance, runner success, production readiness, or live-provider behavior is claimed. Existing dirty work was read as the authoritative review input and was not modified. The only written artifact is this report, created with `apply_patch`.

Reviewed source fingerprints (SHA-256):

| File | Hash |
| --- | --- |
| `account-bridges.component.ts` | `5C590F49FD3A3D82B8260741502D5943F57BCB9ED184CA812FDC9BA93A6D6F53` |
| `account-bridges.component.html` | `3B0A1F26726654D1D0781DFD554BE59D7C450E7E1F5CB3A1CB48233C1569A038` |
| `account-bridges.component.spec.ts` | `5B0BC4142B7FB745E32EABE899127F357F5B4A2A392DDF21563211C84B72BB19` |

The spec changed concurrently during this review to import AccountBridgesModule. Its relevant test sections were re-read, and references/fingerprint above were updated to that revision. No source edits were made by this sidecar. Line references and findings apply to these inspected working-tree versions; subsequent parent changes require rechecking.
