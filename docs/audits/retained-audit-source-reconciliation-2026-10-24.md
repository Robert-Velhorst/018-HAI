# Retained audit source reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This review concerns historical proposal snapshots, not full working-copy deletion clearance.

## Approval proof issuance

Historical head `3399f31bc4b79158fd7b4da36cd99e4faeab44d9` contains two commits affecting 15 paths. Twelve final files match main after ordinary line-ending normalization, including the production approval-proof implementation and issuance/read-scope migrations. Three remaining test differences were checked with explicit transformations against complete file contents:

- The PostgreSQL fixture now requires a dedicated test DSN through `pgtestguard` instead of accepting an arbitrary configured DSN.
- The execution-authorization SQL assertion normalizes CRLF input.
- The migration-chain expectation appends migrations 0131, 0132 and 0133.

No other differences were accepted for those three files. Reintroducing the historical files would remove subsequent main improvements. No production port is needed for this snapshot. This is source preservation evidence, not a real PostgreSQL test run. The local repository also contains other refs that require their own preservation checks.

## Runtime image pin

Historical head `de415ca4c489254ba068dfb46563d7d56631efa0` contains one commit affecting five paths. Main retains its intended pinned Ubuntu runtime stage. The three service Dockerfiles differ only by the subsequent pinned Go builder update. One remaining file is exact; the targeted runtime-image contract test has identical Python AST on main, although its containing file has other later changes. No Docker image was built or executed for this comparison.

## Jasmine proposal: intentionally deferred, not blindly applied

Historical head `801d7040862fe4d219c7265cb12e7d85d76bad04` contains the Jasmine 7.0.2 bump and reporter compatibility adjustment in two package metadata files. Main already has `@types/jasmine` 7.0.0 and HTML reporter 2.3.0, but intentionally retains `jasmine-core` 6.3.0.

The existing main audit, `stable-runtime-dependencies-2026-10-23.md`, and merged PR 330 explicitly retain a runner/Zone migration as unfinished work. The [official Jasmine 7 upgrade guide](https://jasmine.github.io/upgrade-guides/7.0) confirms that the current Karma adapter is incompatible and that Zone.js monkey patching does not work with Jasmine 7. Updating only the HTML reporter does not resolve either boundary.

This review therefore does not replace the supported runner with an incompatible dependency, suppress tests, or claim Jasmine 7 has been integrated. A maintained-runner and Zone migration must preserve the existing test coverage before adoption. The proposal's original head is included in the previously verified `archive/local-preservation-20261024/hai-dependency-review-history` tag; archival preservation is not main integration or deletion clearance.

## Remaining boundary

No source archive, database, transcript history or divergent checkout is cleared by this comparison. Full refs/reflogs, physical files, ignored data, shared metadata and consumers must still be accounted for before any copy is removed. No new dependency installation, app stack, container, agent or provider request was started for these source comparisons.
