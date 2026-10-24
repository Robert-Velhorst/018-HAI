# Canonical content preservation for 39 archived Go additions

## Result

Baseline main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`, freshly rechecked by the frozen archive-input helper on 2026-10-24.

All 39 changed Go additions selected by the declaration-preserved AST category now have full-content comparisons, not merely matching function names:

- 14 files are byte-identical after standard Go formatting, including comments.
- 23 retain every archived formatted nonblank line in order; main only adds lines.
- Two retain those lines after expanding the single-line testing import into a block. Main adds imports and declarations; the historical testing import and all old content remain.
- No formatter rejection or unresolved comparison remains in this selected group.

These are three distinct evidence levels. Only the first is exact canonical equality. Neither insertion-only result means unchanged runtime behavior: new main declarations can intentionally extend the program.

## Method and limitations

Used the existing Go 1.27.3 gofmt executable with stdin/stdout only and no simplify or rewrite flags. Verified executable identity before and after. The existing helper checked frozen index identity, successful add-event type, archived content fingerprints and stable archive size/metadata, and read current blobs from the pinned main revision.

The first pass compared complete formatted output bytes. A second pass used insertion-only sequence comparisons after removing only blank lines; comments were not stripped. The final narrowly labelled normalization expands only `import "testing"` into an import block. The prior AST pass independently established matching archived declarations and no missing archived imports for this group. No arbitrary code rewriting or ignore rule was applied.

Initial and refined private comparison reports were retained rather than overwritten. No source files, Go build cache or module cache were changed by this comparison. Archived source was not executed. No unit, database, provider, browser or deployment acceptance is claimed.

Raw source formatting remains preserved in the retained archives. This receipt does not authorize deleting an archive or an entire checkout, nor cover non-code requirements, other revisions or other changed files.

## Per-file evidence

References are abbreviated content fingerprints from the frozen archive index.

| Archived path | Reference | Comparison |
| --- | --- | --- |
| `backend/internal/memory/repository.go` | `1ddc44f1d1b4` | Old lines retained; main additions |
| `backend/internal/safety/emergency_stop_test.go` | `c44aa4e3b343` | Old lines retained; main additions |
| `backend/internal/ambient/lifecycle_test.go` | `b6b39e5b7eb0` | Old lines retained; main additions |
| `backend/internal/models/autonomy.go` | `8152daabfb4a` | Exact canonical file |
| `backend/internal/task/minimality_test.go` | `8af3ff9be88a` | Old lines retained; main additions |
| `backend/internal/workflowtask/runner_test.go` | `c19333469b6e` | Old lines retained; main additions |
| `backend/internal/pursuit/handler_test.go` | `8c135cda855e` | Old lines retained; main additions |
| `backend/internal/verification/pursuit_link_test.go` | `08d9ad8b1e5c` | Old lines retained; main additions |
| `backend/internal/memoryengine/handler_test.go` | `174800bd5057` | Old lines retained; main additions |
| `backend/internal/models/llm.go` | `af91a25b72f9` | Old lines retained; main additions |
| `backend/internal/automation/automation_handle_test.go` | `8d387fca8f98` | Old lines retained; main additions |
| `backend/internal/phase2/module_test.go` | `a0bb6a658774` | Old lines retained; testing import expanded |
| `backend/internal/metrics/exporter_test.go` | `f3264ffec831` | Old lines retained; main additions |
| `backend/internal/planningoptimizer/service_test.go` | `c1082ffb95bd` | Old lines retained; main additions |
| `backend/internal/browserverify/repository.go` | `24c476b67c2e` | Exact canonical file |
| `backend/internal/research/handler.go` | `ee467bab5857` | Old lines retained; main additions |
| `backend/internal/braincatalog/collection_review_test.go` | `f3b816a7b38f` | Old lines retained; main additions |
| `backend/internal/presidio/service_test.go` | `8877a26a024a` | Old lines retained; main additions |
| `backend/internal/presidio/handler_test.go` | `b990444e3262` | Exact canonical file |
| `backend/internal/promptfoo/handler_test.go` | `b7c011d6ade0` | Old lines retained; main additions |
| `backend/internal/whispercpp/service_test.go` | `4e3be221fd91` | Exact canonical file |
| `backend/internal/whispercpp/handler.go` | `2aa91802f530` | Exact canonical file |
| `backend/internal/langfuse/handler_test.go` | `8e677123feb8` | Exact canonical file |
| `backend/internal/serena/service_test.go` | `f6a1d8fde6cd` | Old lines retained; main additions |
| `backend/internal/anythingllm/service_test.go` | `c9b636a59b18` | Exact canonical file |
| `backend/internal/garak/service_test.go` | `d3c9eb24b350` | Exact canonical file |
| `backend/internal/garak/handler_test.go` | `587ded2846f8` | Exact canonical file |
| `backend/internal/garak/handler.go` | `63cc6834e6ec` | Exact canonical file |
| `backend/internal/models/miniswe.go` | `c555b4b6d713` | Exact canonical file |
| `backend/internal/mlflow/service.go` | `530aa9bf0576` | Exact canonical file |
| `backend/internal/mlflow/service_test.go` | `dbeae83a70b5` | Exact canonical file |
| `backend/internal/autogencompat/handler.go` | `7c1e0f6dec83` | Old lines retained; main additions |
| `backend/internal/models/brain_catalog.go` | `efa1dba80196` | Old lines retained; main additions |
| `backend/internal/braincatalog/repository.go` | `db01af339f8a` | Old lines retained; main additions |
| `backend/internal/gitleaks/handler_test.go` | `25e78fcbcb7b` | Old lines retained; main additions |
| `backend/internal/syft/handler_test.go` | `beb3afb37444` | Old lines retained; main additions |
| `backend/internal/gosec/handler.go` | `6971a104d7ca` | Exact canonical file |
| `backend/cmd/main_test.go` | `e325cc1d195f` | Old lines retained; main additions |
| `idp/internal/infra/kafka_test.go` | `e7d911b780e7` | Old lines retained; testing import expanded |

## Remaining scope

The other 212 changed Go additions in the 251-variant comparison remain outside this canonical-content cohort: 173 have changed declarations without absent identities, 36 have absent identities with separate dispositions, and three historical snapshots do not parse. Some paths overlap broader reviews; these counts must not be added to earlier patch cohorts as unique global files.

Whole-history requirement coverage, original local source copies, exact-head CI, main integration of prepared repairs and independently verified local deletion remain open.

