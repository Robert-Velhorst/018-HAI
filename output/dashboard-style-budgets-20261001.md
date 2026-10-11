# Dashboard Style Budgets: Source Handoff

Date: 2026-10-01
Workspace: `%HAI_WORKSPACE%`
Branch observed: `codex/hai-runtime-release`

## Outcome

Partial source-only consolidation across all six assigned SCSS files. This is NOT a claim that all six warnings are fixed. No compiler, production build, executable tests, or screenshots were run. The large Pursuits, shell, Task Blueprint, and Workflow Engine overshoots remain too large to consider resolved by this bounded pass. Control Center and Connected Sources need actual compiled-size measurements before their warnings can be called cleared.

No budgets were raised, no route styles were moved into eagerly loaded global styles, and no functional style sections were removed. Existing changes were retained. No shared file outside the six assigned stylesheets was changed; shared extraction would require coordination first. No descendants, commits, pushes, installs, service/container operations, localhost calls, or provider/account calls were performed.

## Evidence And Ownership

Read `output/runtime-outcome-frontend-final-build-20261001.log`, which records completion at `2026-10-01T09:10:52.062Z`. Its warning threshold is 20.00 kB. The current `frontend/angular.json` still configures `anyComponentStyle.maximumWarning = 20kb` and `maximumError = 48kb`.

The assigned shell path does not exist under `layout/app-shell`; the actual source and logged warning path are `frontend/src/app/control-room/app-shell.component.scss`.

| Component | Logged Compiled Size | Logged Overshoot | Source Bytes Before | Source Bytes After |
| --- | ---: | ---: | ---: | ---: |
| Pursuits | 45.79 kB | 25.79 kB | 55,255 | 53,845 |
| App Shell | 26.79 kB | 6.79 kB | 31,640 | 30,121 |
| Workflow Engine | 23.92 kB | 3.92 kB | 28,234 | 27,882 |
| Task Blueprint | 27.28 kB | 7.28 kB | 34,101 | 33,623 |
| Connected Sources | 20.93 kB | 930 bytes | 24,670 | 24,216 |
| Control Center | 20.61 kB | 610 bytes | 23,110 | 22,269 |

Source-byte reductions are NOT compiled-byte measurements. Nested Sass selector consolidation can reduce emitted repetitions without materially reducing source bytes; Angular/esbuild must measure the actual result.

## Changed Paths

- `frontend/src/app/control-room/app-shell.component.scss`
- `frontend/src/app/pages/pursuits/pursuits.component.scss`
- `frontend/src/app/pages/workflow-engine/workflow-engine.component.scss`
- `frontend/src/app/pages/task-blueprint/task-blueprint.component.scss`
- `frontend/src/app/pages/connected-sources/connected-sources.component.scss`
- `frontend/src/app/pages/control-center/control-center.component.scss`
- `output/dashboard-style-budgets-20261001.md`

## Changes And Findings

1. Factored repeated selector prefixes/suffixes with `:is()` where the alternatives have equal specificity. No `:where()` specificity reductions, broader class-prefix matching, or selector renaming were introduced. Kept distinct specificity groups separate. This reduces repetitive shared-header, status, typography, and portfolio selectors while retaining their matched elements.
2. Consolidated nested Sass alternatives with `:is()` to avoid repeating parent selectors for each nested child rule. Existing encapsulation settings remain untouched: the shell is global; route components retain their existing emulated encapsulation. Existing global token names and theme declarations remain unchanged.
3. Control Center: removed the redundant first blocked next-action declaration, which was repeated later. Derive `--tone-soft` from `--tone` on the existing card/icon/inspector element instead of redeclaring identical mixes in each same-element tone variant. Color choices and the 15% mix are unchanged; parent should specifically check green/gold/red variants.
4. Connected Sources: removed the source-approval footer's duplicate flex declarations already supplied by the existing `.form-footer` rule. Consolidated the identical muted 12px/1.4 typography of routing-result secondary text and projection-warning text, preserving the warning's separate margin.
5. Pursuits: combined the identical muted-surface, border, radius, and padding declarations for detail-summary articles, link-manager details, and source-grid articles. Inspected the current template: link-manager and linked-records are separate sections, avoiding an ancestor/cascade collision with the latter's details styles.
6. Workflow Engine: combined the identical shared disclosure-popover and legacy tools-body surface/position declarations. Kept the body's flex layout and gap separate and retained the existing toolbar disclosure selector required by the source contract.
7. Task Blueprint: merged adjacent identical inspector-family selector lists that separately declared text color and surface/border color.

The checked-out Angular builder's `budget-stats.js` measures compiled component stylesheet output bytes, not SCSS file length. Source compression alone is not evidence that its budget passed. No production-readiness conclusion follows from these edits.

## Inspection Performed

- Used the installed `ecc:angular-developer` skill and its component-styling guidance, the local Angular 22 package/configuration, component metadata, relevant templates read-only, global token definitions, and local builder budget implementation.
- Confirmed all six stylesheets already had substantial changes relative to HEAD before this work. HEAD is therefore NOT a trustworthy pre-worker visual/equivalence baseline.
- Compared the ordered media-query conditions against the pre-worker source: unchanged for all six files, including existing forced-colors and reduced-motion sections. Basic/Advanced rules, overflow protections, focus declarations, and responsive sections were retained. This is source inspection, not browser evidence.
- Ran the following static whitespace check successfully (exit 0). Git printed only its existing LF-to-CRLF conversion notices; no whitespace-error findings were returned.

```powershell
git diff --check -- frontend/src/app/control-room/app-shell.component.scss frontend/src/app/pages/pursuits/pursuits.component.scss frontend/src/app/pages/workflow-engine/workflow-engine.component.scss frontend/src/app/pages/task-blueprint/task-blueprint.component.scss frontend/src/app/pages/connected-sources/connected-sources.component.scss frontend/src/app/pages/control-center/control-center.component.scss
```

## Parent Verification Commands And Regressions

Run serially, only when host capacity permits. These commands are proposed handoff steps, NOT executed results from this worker.

```powershell
# From the workspace root; source contracts do not require browser/service startup.
python scripts/test_ci_contract.py

# From frontend; bound workers and heap in the parent's approved test window.
$env:NG_BUILD_MAX_WORKERS = '1'
$env:NODE_OPTIONS = '--max-old-space-size=1024'
npm.cmd run build -- --configuration production
```

Do not run the build while free RAM remains below 1 GB. Record the resulting sizes and all warnings, not merely the exit code: Angular component budget warnings need not fail the build.

Required visual regression matrix, using the parent's existing approved browser/runtime setup:

- All six views at desktop and 390px mobile, in light/dark and Basic/Advanced modes. Confirm navigation rail/menu, footer/actions, grids, labels, and popovers are not clipped or overlapping.
- Keyboard focus on source actions, workflow disclosure, pursuit cards/forms, Task Blueprint composer/approvals, dashboard cards, and shell navigation. Check reduced-motion and shell forced-colors behavior.
- Control Center green/gold/red/default card and inspector variants, plus urgent/default item icons: foreground and soft backgrounds must retain their prior meaning and contrast.
- Pursuits portfolio modal and resource-reservation stale state; Connected Sources approval footer, sync recovery, warning text; Workflow Engine tools popover and mobile inbox; Task Blueprint inspector and conversation in both modes.

No focused executable regression file was added: test-file ownership was outside this worker's permitted paths. The matrix above identifies the focused regressions for the parent to run or add under its own ownership.

## Remaining Work

Keep this task open until a fresh production build proves all six component styles at or below the unchanged warning threshold and the visual matrix passes. The original large overshoots will need additional work; these changes are incremental, not a six-warning solution. Any broader shared-style extraction must be coordinated before touching a shared file and must preserve lazy route loading and component isolation. Do not solve the warnings by transferring all feature CSS to an eager global bundle.
