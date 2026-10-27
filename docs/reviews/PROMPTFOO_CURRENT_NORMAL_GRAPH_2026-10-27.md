# Current Promptfoo normal dependency graph review

The default runner pins Promptfoo 0.125.0 and includes a Get-URI/Basic-FTP override. Public registry metadata currently reports Promptfoo 0.125.1, Azure Identity 4.14.0 and MSAL Node 7.1.1. Azure Identity still declares MSAL ^6.0.0; Get-URI 8.0.1 still declares Basic-FTP ^5.3.1 while its public latest stable major is 6. These publisher constraints require actual graph evidence and potentially compatibility ports; an override is not qualification.

The added owned hosted-worker job selects the highest public nondeprecated stable versions for both runner declarations, removes overrides only in its disposable candidate, updates the version-derived production ENGINE string, and runs normal npm resolution and a complete installation. It retains the unfiltered audit including optional dependencies, compares every previously locked package path for lower versions, records removed paths, and catalogs every resolved public package namespace against the highest stable publisher release. Missing registry evidence remains an error rather than zero findings.

The original node test command and Docker build command run unchanged on the generated candidate. The original Dockerfile runs its existing application cases, version check and network-isolated six-request synthetic CLI fixture. Original cases, helpers, fixtures, assertions, selectors, timeouts and Dockerfile are untouched. The three generated candidate payloads, all source hashes, exact commands, complete audit, namespace catalog, version deltas and any image inspection are retained even when a gate fails.

Application manifests, lock, runtime source and existing CI remain unchanged in this proposal. Generated outputs are not adopted until independently reviewed; a lower version or inherited advisory is not accepted. The strict workflow must fail when audit, stable-version, no-lowering, install or original-runner gates fail. Passing local-model fixtures would not establish full publisher SDK, bundled-component/native-image, real provider or live-data acceptance. Those remain explicitly unqualified.

All execution occurs on the owned hosted GitHub worker. No local application, package manager, image, scanner, compiler, SDK or test execution is required. No main merge, manual deployment, database migration, paid feature or permission change is included.

## Get-URI source compatibility preparation

The additional owned Linux SDK job compares two separate checkouts of official Get-URI 8.0.1 publisher commit b7e5f7ccce1a3ac5b339cc4c587974e8989cbc16. Its GitHub-verified commit tree and all 202 original source payloads are bound before execution. The annotated release tag itself is unsigned; no signed-tag claim is made.

The baseline preserves its original frozen lock. The port uses a normal pnpm add to change only Get-URI's Basic-FTP declaration to ^6.2.3 and regenerate the SDK lock, followed by the original frozen install. Both workspaces then run the unchanged full pnpm test, lint, format check and original engine/license commands on Node26.12.1 with the publisher-pinned pnpm10.30.3. Original cases, fixtures, helpers, assertions, case selectors, test timeouts, compiler flags and source protocols remain unchanged. Separate workspaces prevent a previous build's local outputs from qualifying the port.

The actual installed Basic-FTP package bytes are compared with their integrity-verified public release tar. Additional private pnpm packaging compares every baseline/port archive payload. Only the explicit dependency declaration should differ; other changes remain visible and fail the narrow port gate. Generated packages are private compatibility artifacts, not official new upstream releases, and are not adopted into the application.

This Linux preparation does not qualify the publisher's other OS/version matrix, Basic-FTP's own complete SDK, every bundled/current toolchain component or the original VPN-dependent end-to-end and Vercel-dependent custom pack workflow. Those original sources remain untouched and their gates remain open. No VPN, Vercel or live provider deployment is invoked, and no credentials are requested or supplied.
