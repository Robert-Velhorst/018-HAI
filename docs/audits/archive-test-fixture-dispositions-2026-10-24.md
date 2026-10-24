# Archived test-fixture declaration dispositions

## Scope

Baseline main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This static pass reviews the remaining 22 non-test-function identities in nine test files from the refined absent-declaration cohort. The nine actual historical test functions have their own successor receipt. All nine current files match main after repository Git filtering.

Archived successful additions were reread at frozen offsets with content identity and stable archive metadata checked. Current helper implementations and assertion call sites were inspected. No old fixture was executed or restored, and no unit, integration, SMTP, Redis, Kafka or provider test was run in this pass.

## Dispositions

| Archived path and reference | Identities | Current disposition |
| --- | --- | --- |
| `idp/internal/app/authentication/auth_service_test.go`, `8a0d09dbb4c6` | `noopSender`, `noopSender.Send` | The historical always-successful sender is no longer required by this file's fixtures. Dedicated account-created and account-blocked fixtures exist in the HTTP/Redis and password-reset integration tests and record topics/calls. Those environment-dependent tests were not run here. |
| `backend/internal/memory/service_test.go`, `1475f769f82b` | `errNotFound`, `errNotFound.Error` | Current fake repository failures return `gorm.ErrRecordNotFound`. Retain standard repository error identity rather than an isolated custom error-string type. |
| `backend/internal/task/service_test.go`, `f5c408a70432` | value-receiver `fakeMemoryService.Create`, `FindByID`, `Retrieve` | All three names remain with pointer receivers. Receiver shape is part of the AST identity key. The current fake stores records and owner-aware helpers use/capture this state. Do not restore value-receiver copies. This does not certify every fake mutation's production authorization semantics. |
| `backend/internal/source/service_test.go`, `66c82d7c1fcb` | `osWriteFile` | Fixtures call `os.WriteFile` directly; the private alias is unnecessary. |
| `backend/internal/agentcycle/handler_test.go`, `3b1f27e5d15c` | `contains`, `stringContains` | Current owner-scope and error-redaction assertions use `strings.Contains` instead of handwritten substring search. |
| `backend/internal/ambient/handler_test.go`, `63066efb2c75` | `capturingAmbientService` plus `Accept`, `Dismiss`, `Overview`, `OverviewForOwner`, `Scan`, `ScanForOwner`, `UpdateNeedForOwner` | Replaced by the current resolution fixture. Accept/dismiss retain owner/actor capture and assertions; overview/scan/preference interfaces remain, with summary projection support. These fakes are not a complete database authorization implementation. |
| `backend/internal/braincatalog/handler_test.go`, `b6be10708a32` | `contains`, `find` | Current catalog-content, caching and provenance assertions use `strings.Contains`. Do not restore private substring algorithms as missing catalog functionality. |
| `backend/internal/evidently/service_test.go`, `4a38848b9d38` | `errorsIs` | Unsafe/disabled fixture assertions use standard `errors.Is` identity/chain matching, not a custom comparator. |
| `idp/internal/app/config/authentication_config_test.go`, `466c049d6df7` | constant `testJWTSecret` | The same name exists as a variable initialized from a generated synthetic fixture. Const-to-var is an AST identity change, not missing authentication configuration coverage. No credential value is reproduced here. |

## Verification boundary

Source inspection and filtered main comparisons establish these dispositions only. No Go build cache was regenerated and no tests were rerun. Earlier results keep their original scope and timestamps.

Together the receipts address 23 absent production identities, nine actual test functions and these 22 fixtures: 54 identities across 36 absent-identity variants. They do not clear other changed declarations, revisions, comments/requirements, invalid snapshots or complete transcript records. No source or archive deletion is authorized by this count.
