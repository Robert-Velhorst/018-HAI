# Archived regression-test successor review

## Scope

Baseline: remote main `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
The refined Go declaration comparison reported nine historical `Test...` identities absent from their original files, across eight archived additions. This receipt records their assertion-level disposition, not just name similarity. Other missing identities in test files are fixtures/helpers and remain a separate review cohort.

Archived successful add events were reread using the frozen index and verified content fingerprints and stable archive metadata. No archived test was executed. All eight current test files match main after Git filtering, not necessarily as raw working bytes.

## Dispositions

| Archived path and reference | Historical test | Current disposition |
| --- | --- | --- |
| `backend/internal/agentruntime/runtime_test.go`, `9d2d02ba1935` | `TestOdysseusAdapterUsesAgentModeWithoutBash` | Intentionally superseded by `TestOdysseusAdapterBlocksExecutionUntilToolCallsAreMediated` and `TestOdysseusAdapterDoesNotContactRunnerWhenHighRiskToolsAreConfigured`. The old test expected successful agent-mode execution. Current policy blocks unmediated tool execution rather than treating a no-bash flag as sufficient authority. Do not restore the historical success expectation. |
| `backend/internal/assistant/handler_test.go`, `8c2d8df492c4` | `TestCommandHandlerRejectsGlobalCycleForNonOwner` | Current identity tests reject unauthenticated requests; authenticated personal cycles pass only the verified subject into the cycle runner. `TestCommandHandlerAllowsAuthenticatedPersonalCycleForOperator` tests personal scope, not permission for a global cycle. The historical role-only request had no verified subject. This comparison does not certify installation-admin-only global authorization. |
| `backend/internal/ambient/handler_test.go`, `63066efb2c75` | `TestAmbientResolutionHandlersRequireVerifiedOwner` | Renamed `TestResolutionHandlersRequireVerifiedOwner`: both accept and dismiss reject unauthenticated calls before reaching the service. |
| `backend/internal/ambient/handler_test.go`, `63066efb2c75` | `TestAmbientResolutionHandlersUseVerifiedOwner` | Renamed `TestResolutionHandlersUseVerifiedOwner`: both operations receive the verified subject as owner and actor. |
| `backend/internal/router/interactive_permissions_test.go`, `0d751a21a4fa` | `TestInteractiveExecutionRoutesRequireSignedApprovalPermission` | Superseded by `TestConsequentialTaskExecutionAndReviewRequireOwnerApproval` and the separate planning-permission test. The historical operator execution/approval success cases now require owner role; viewers retain review-queue read access. Do not port the old operator authorization expectations. Owner role is not itself proof of first-run installation-admin identity. |
| `idp/internal/app/authentication/auth_middleware_test.go`, `8056186556df` | `TestAuthMiddlewareRefreshesBeforeResolvingExpiredAccessIdentity` | Expanded into `TestAuthMiddlewareRefreshesBeforeResolvingIdentity`: expired-access and refresh-only cases resolve identity/session from the refreshed token, preserve session version and set a refreshed cookie. |
| `backend/internal/a2abridge/service_test.go`, `28266afb8437` | `TestHandlerProvidesCardAndTokenBoundedTaskSend` | Superseded by `TestHandlerProvidesCardAndTokenBoundedSendMessage`. Card discovery, token rejection and sanitized planning remain, while the supported envelope is SendMessage and separate tests reject the old protocol shape. |
| `backend/internal/llm/maintenance_scheduler_test.go`, `a8649f1239d1` | `TestModelMaintenanceSchedulerIntervalIsBounded` | The historical configurable hourly polling interval is not the current scheduling contract. Current tests require fixed daily model freshness, durable due scheduling with daily fallback and separately bounded failure retry. Do not restore a five-minute-to-daily polling knob in place of current due scheduling. |
| `backend/internal/frameworkregistry/handler_security_test.go`, `9ea8988e8b97` | `TestPublicSelectionIgnoresClientApprovalAndRiskAssertions` | Strengthened to `TestPublicSelectionRejectsClientApprovalAndRiskAssertions` and `TestPublicSelectionRejectsEveryTrustedOrAuthorityRaisingField`: reject forged authority fields, then accept a clean request without contaminating the server decision. |

## Current verification

Three native Go 1.27.3 commands completed successfully with vet enabled, offline module resolution and readonly module files. All 18 selected top-level tests and 30 nested cases passed, with no skips:

- Backend: two ambient, three assistant, two framework-registry, two route-permission and three model-maintenance tests.
- Identity: one refresh-before-identity test with two nested session cases.
- Runtime/protocol: two blocked-Odysseus tests and three A2A handler/replay/protocol tests.

The nested cases cover six forged framework authority fields, seventeen route/role cases, five due-scheduling cases and two refresh paths. The fixtures are in-process or ephemeral loopback services with synthetic identities. No external agent, provider, real user database or live deployment was exercised.

## Preservation boundary

No production behavior, role permissions, network configuration or provider settings changed in this review. No source, transcript or evidence files were deleted. These nine test-name dispositions do not establish whole-file equivalence, full archive integration, database acceptance or end-to-end release readiness.
