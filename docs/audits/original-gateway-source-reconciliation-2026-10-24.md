# Original gateway source reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Pre-fix prepared checkpoint: `a19d7c11659c444d5536807dd4dec47e939b234a`.

## Original tracked input inventory

A fresh content read verified all 244 physically present tracked inputs against the frozen original inventory. Their raw SHA-256 values were unchanged. Against both reviewed main and the pre-fix prepared tree, 243 files differ and one path is absent. This is literal inventory, not a conclusion that 243 changes are missing: subsequent main improvements and historical source changes still require individual review. The 448 already-missing tracked files were not staged as deletions. Untracked content is outside this pass.

## Generated gateway configuration successor

The absent path is `nginx-config/nginx.conf`. The canonical stack renders this file from `nginx.conf.template` with environment substitution and restrictive permissions. Its generated output is ignored and untracked; restoring the older static file would reverse this secret-handling boundary.

Eight selected gateway/Compose/ignore inputs matched the pre-fix prepared tree. Four already match reviewed main; the gateway template, gateway tests, generic automation route and local Compose differ from main. These are pending prepared changes, not already-main claims.

All 12 legacy location blocks were inspected with exact block markers, including a distinct root location rather than a prefix match on the API catch-all. Every original in-block directive is present in the successor after the explicit replacement of a hard-coded example backend key with its environment placeholder. Docker DNS resolution, version hiding, unauthorized-login handling and generated-route inclusion remain present. The frontend target intentionally uses port 8080 rather than the historical port 80; the existing contract checks the frontend listening port. Removing the upload authentication directive causes the legacy-route comparator to reject the mutated template.

## Selector regression correction

The existing test helper searched by unrestricted substring. As a result, selecting `location /` could choose `location /api/v1`, and a comment or longer route prefix could satisfy an intended marker. The prepared correction anchors markers to line starts and requires an actual block boundary. Existing callers that include the opening brace remain supported; a missing marker continues to raise `ValueError`.

Three synthetic regression tests fail with the original selector and pass with the corrected selector. The full existing static gateway suite plus these regressions passes all 27 tests with no failures, errors or skips. Selected input bytes and the original generated-config source remained unchanged during execution. Only read-only Git subprocesses were used by the tests.

## Limits

These are source and static-contract checks. No Nginx executable, container, browser, app stack, database or external provider was started. Nginx syntax/runtime acceptance, rendered-secret handling and live login/upload behavior remain separate gates. This review does not clear the original working copy, its untracked files, archives or other local sources for deletion.
