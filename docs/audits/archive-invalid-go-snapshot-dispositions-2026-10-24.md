# Invalid historical Go snapshot dispositions

## Scope and source integrity

Baseline main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Three of the 251 changed Go additions do not parse as archived. All three current files parse with the installed Go 1.27.3 formatter and match main after Git filtering. Successful add-event identity, indexed content fingerprint and stable archive metadata were rechecked by the frozen archive-input helper.

Parser failure is not treated as permission to ignore feature intent or delete evidence. The original snapshots remain unchanged in the retained archives. This is static review, not compilation or execution of archived code. These three snapshots are distinct from the 47 malformed compacted JSONL records described elsewhere.

## Semantic service tests

- Path: `backend/internal/semantic/service_test.go`; reference `1a91bc53be3e`.
- Archived length: 24 lines. The formatter reports EOF syntax errors at line 24, column 4.
- Visible tests: `TestValidateLocalURL` and `TestVectorLiteralAndInputTrimming`.
- Current main retains the same allowed/rejected endpoint cases, vector literal assertion and rune-trimming assertion in complete tests.
- An in-memory diagnostic appended one closing brace and expanded only the testing import to a block. That probe parses, and all its formatted nonblank lines are retained in order by main with additions. No repaired historical file was written or executed.
- This proves preservation of the visible assertions, not reconstruction of any unwritten continuation of the archived test.

## Model-maintenance scheduler

- Path: `backend/internal/llm/maintenance_scheduler.go`; reference `b0e091f38eab`.
- Archived length: 65 lines. Parser errors begin at line 40, column 3, around the unterminated goroutine/loop structure; later diagnostics cascade.
- The historical scheduler attempted a startup sweep, periodic configured refresh and an emergency-stop callback. Its callback could be nil, its sweep did not propagate the scheduler context, and the interval helper implemented hourly polling bounded from five minutes to a day.
- Main keeps `StartModelMaintenanceScheduler` and `modelMaintenanceSchedulerEnabled`, requires a nonnil safety callback and context, uses a single-flight lifecycle worker, propagates cancellation and schedules from durable due timestamps and wake events with a daily fallback. The old interval helper is intentionally superseded, not copied into current scheduling.
- Inspected the current startup, due-delay and waiting paths. This does not prove live provider freshness or a real model update. Do not restore the old loop or its permissive missing-gate behavior merely to make the historical snapshot parse.

## Agent-runtime tests

- Path: `backend/internal/agentruntime/runtime_test.go`; reference `e407e84b85ee`.
- Archived length: 120 lines. The record ends inside `TestRegistryStopTaskCancelsActiveRuntimeExecution`, immediately after a select-case header. Formatter diagnostics point to line 120, column 29.
- Four complete visible tests cover approval requirement, emergency stop, authorized execution and configuration/audit block messages. Main retains those test names and intents, with verified task-bound authorization replacing trust in a caller-supplied approval flag. The authorized test now checks runtime/task/owner/project/prompt binding and proof delivery; an additional test rejects forged approval claims.
- Main also retains the cancellation test. It rejects a cross-owner stop and distinguishes `cancellation_requested` from verified downstream completion; interrupted execution is indeterminate. Keep these explicit uncertainty states rather than reverting to the old stopping status.
- The archived fragment does not record its final post-cancellation assertions. No missing assertion or successful outcome is inferred from that absent tail.

## Remaining gates

All three now have explicit source-intent dispositions, but none is raw-source-equal to a complete historical program. No fresh unit, database, browser, provider or deployment test ran in this pass; no Go build/module cache was regenerated. Other changed declarations, complete transcript requirements, publication, exact-head CI, main integration of prepared repairs and deletion verification remain open.
