# Frontend delivery and manifest reconciliation

## Reviewed original files

The complete original `frontend/package.json`, `frontend/e2e/package.json` and
`frontend/nginx-custom.conf` were compared with the integration versions.
The newer implementation is retained; no older runtime or dependency versions
are restored and no packages are installed by this reconciliation.

The application retains all thirteen original native test selectors through
the shared sixteen-selector `test:native` command, followed by non-watching
Angular tests. The five focused compiler invocations remain intact. Theme/icon
preflight and lockfile/theme/history contracts are additions, not replacements
for the original release checks. Existing main-branch runtime, package-manager
and dependency declarations remain unchanged.

The separate browser-test manifest retains all six original commands and its
private-package boundary. Its newer compiler aliases, runtime declarations and
dependency versions remain as already present on main. Two additional native
contracts verify command retention, manifest/lock-root agreement, direct lock
records and registry integrity. This does not audit the implementation of every
transitive package or prove browser acceptance.

The frontend Nginx configuration retains the original response compression,
hashed-bundle caching, unversioned-asset revalidation and document no-store/SPA
fallback rules. Its intentional newer differences are port 8080 and the local
`/healthz` response. Two native contracts now cover the runtime port, bounded
compression and frontend-only health result. A frontend health response does
not establish backend, identity-provider or external-provider readiness.

## CI correction

CI at published head `3056fa0e3d22c34d764eaa4d01ec4a52850ae279` exposed a stale
workflow assertion: it searched for native test filenames directly inside
`npm test` instead of following the new shared command. The assertion now
checks the complete sixteen-file command, sequential execution, Angular
continuation and the actual CI invocation. Twenty-four negative variants
remove selectors or weaken local/CI commands; each must be rejected. The
existing focused compiler, production-build and full Angular gates remain.

Local results: 132 Python CI-contract tests and 266 frontend native tests pass,
with no skips or failures. Six focused lockfile and six release-cache tests
also pass as subsets of that native run. These are not a new production build,
rendered browser pass, deployed acceptance or a successful new-head CI run.
The already running CI is allowed to finish before another push.

## Separate dependency gate

The same CI run independently rejected the Promptfoo runner's production
dependency graph. A read-only lockfile audit confirmed Zod 4.6.5 and the
high-severity [GHSA-mrhx-c7xc-m3w5 advisory](https://github.com/advisories/GHSA-mrhx-c7xc-m3w5),
whose patched version is 4.6.6. The owner subsequently approved this exact
security update. Only Zod's version, registry tarball URL and integrity fields
in the runner lockfile change; the package manifest and every other dependency
record remain unchanged. The official registry metadata supplies the patched
tarball and integrity value. Both existing consumer ranges accept 4.6.6.

The post-patch production lockfile audit reports zero high and zero critical
advisories. It still reports one separate moderate advisory for `ip-address`
([GHSA-rxp7-6hxx-x8q5](https://github.com/advisories/GHSA-rxp7-6hxx-x8q5));
this narrowly approved change does not fix that dependency. The default audit
therefore returns a nonzero result, while the unchanged high-severity release
threshold passes. Three local runner contracts pass. These contracts do not
install or exercise the complete Promptfoo dependency graph, build its image,
or run a real model evaluation. Exact-head CI must verify the image. No global
npm upgrade, provider configuration change or live deployment was performed.
This remains separate from the frontend workflow assertion correction.

Original sources, other clones and transcript archives remain retained. These
three file reviews do not establish that the entire original checkout or any
archive can be removed. Publication, verified preservation on main and local
deletion remain separate gates.
