# Stable runtime and dependency follow-up — 23 October 2026

## Source baseline and changes

This draft integrates main b4f4bc91206eb58be4d6f412c0829c498c9f4889 and the prepared Node-security head a0d7cf526084c2933c971fbf8deb8228c1b585c4 without rewriting either history. Existing main features remain the baseline. The Node update selects 26.12.1 across the runtime files already reviewed in #329.

Official metadata for 67 direct npm/Go declarations was queried against that main snapshot. Thirteen version gaps were found. This follow-up selects the new stable @angular/cdk, @angular/build and @angular/cli 22.2.4 releases, and Promptfoo 0.125.0. Other Angular framework packages remain at their independently published current 22.2.3 releases. The existing Vite 8.3.5 override and security lock updates remain present.

The Promptfoo runner now reports the installed target version, with a regression tying its engine string to the exact manifest pin. Its Docker build requires the actual CLI version to match that pin. A new CLI compatibility fixture runs with Docker build networking disabled, starts only a synthetic loopback OpenAI-compatible endpoint and exercises the existing six-case fixed suite through the real CLI and result parser. It rejects unexpected requests and emits only aggregate compatibility metadata. Its deterministic responses are not evidence of model quality, safety effectiveness or real provider acceptance.

## Verification available before the new hosted run

- Frontend lock generation: 473 packages audited, zero reported vulnerabilities.
- Linux-targeted Promptfoo lock generation using the already cached digest-pinned Node 26.12.1 image and npm 12.2.1, with lifecycle scripts disabled: 597 packages audited, zero reported vulnerabilities. No new local image was pulled.
- All direct manifests agree with their lock roots; selected targets resolve to 22.2.4 / 0.125.0.
- 181 frontend source-contract tests pass under actual Node 26.12.1. These used the existing local dependency installation; they do not qualify the newly selected Angular build packages.
- Four Promptfoo source contracts pass, including exact engine metadata. CLI fixture syntax checks pass.
- New Angular framework/browser builds, actual Promptfoo 0.125.0 CLI execution and the full integrated hosted suite remain pending. Local image builds were omitted because of disk headroom.

## Mandatory remaining gaps

This is not an all-components-current claim. Seven backend Go modules in the audited main snapshot still have newer stable releases. TypeScript 7.0.2 is newer than the frontend compiler package, but Angular build 22.2.4 explicitly requires the TypeScript >=6.0 <6.1 API. The newest public TypeScript 6 API remains 6.0.3. A supported native-compiler/API integration still needs separate work.

Jasmine 7.0.2 is newer than the current Jasmine 6.3.0 dependency, but upstream explicitly states that Jasmine 7 is incompatible with Karma and Zone.js. The frontend uses both and has extensive existing test contracts; upgrading requires a maintained-runner/Zone migration with coverage retained. The current supported runner is preserved while that migration remains unfinished.

Fresh npm audits do not qualify every OS package, embedded native library or model artifact in the runtime images. Live providers, deployment, real databases and operator data are unmodified. All deployment directives remain Hetzner-targeted. No merge is performed by this draft.

Primary evidence: official npm package metadata; https://jasmine.github.io/upgrade-guides/7.0 ; https://registry.npmjs.org/%40angular%2Fbuild/22.2.4 .
