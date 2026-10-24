# Frontend bootstrap, routing and build configuration

This bounded review compares the original `app.module.ts`,
`app-routing.module.ts` and `angular.json` variants with the canonical frontend.
It does not replace the remaining frontend, archive or whole-checkout review.

## Retained routes and shared registration

All 29 original authenticated route entries retain their guard and lazy-loader
syntax. The current configuration adds `operational-brain` and `hire-ai`, bringing
that list to 31. The login route remains outside the operational shell; the shell
adds the authenticated-theme child guard without replacing per-route auth guards.
The `/plans` URL still loads the plan-coordination module rather than requiring
the user to adopt a new URL.

Root-module icon declarations move into `hai-icons.ts`, retain a compatibility
reexport, and remain the `NZ_ICONS` provider value. All ten original shared icons
remain registered. `ControlRoomModule` is now included by the root module. Keep
this composition rather than restoring the old inline, smaller icon list.

## Build configuration disposition

The current JSON configuration deliberately narrows production SVG copying to
the checked reload fallback, adds the non-injected authenticated-theme bundle,
disables critical-CSS inlining, and tightens the initial bundle error budget from
`2mb` to `1.02mb`. The test asset glob narrows to outline icons. Other original
configuration values are retained in the compared JSON structure.

The existing icon checker passed locally and confirms that static icon references
are registered. The theme checker passed with 27 package styles accounted for,
including eight login packages in the initial theme. These source/config checks
are not a new production build, a bundle-size measurement or browser acceptance.

## Added independent regression contract

The earlier Angular route test derived both compared URL lists from the same
configuration, so losing a route could leave the comparison green. A native
TypeScript-AST contract now carries an independent retained URL baseline and
checks lazy import targets, unique paths, auth guards, shell/theme/login
separation, original icon coverage, root icon provision and the reexport.
In-memory negative cases remove a URL, remove a guard, change a module target and
duplicate a URL; all four mutations are rejected without changing source files.

The five new contract tests passed. Their import is wired into the existing
native control-room test entry, which the normal frontend test command runs.
The combined control-room and theme-manifest run passed 133 tests with zero
failures or skips. These are parser/config and component-logic checks, not rendered
screenshots, real login, keyboard acceptance, or navigation against a live server.

Original variants remain intact. No dependency is installed or upgraded, no URL
or auth policy is changed, and no file or worktree becomes deletion-eligible from
this report alone.
