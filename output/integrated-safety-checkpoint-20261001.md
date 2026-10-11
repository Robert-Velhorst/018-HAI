# HAI integration safety checkpoint - 2026-10-01

## Scope and outcome

This checkpoint continues the broader production-readiness work. It does not
redefine HAI's completion or claim production readiness. No commit, push, personal
installation restart, account/provider enablement, paid capacity change, or
external effect was performed. Unrelated dirty work and Playwright artifacts were
preserved. Agents handled independent runtime metadata, privacy fixes, worker
acceptance, CI/isolation, installer review, and safety review; their individual
reports distinguish authored files and verified evidence.

## Genuine remaining execution blocker

The retained synthetic stack `6cfa099c027a455bb3cba587f5b1f0ff` demonstrated exact
source registration, durable job completion and same-source extraction, workflow
selection, mandatory pre-approval refusal, and actual UI owner approval. Focused
operator run 3 still failed its terminal-completion assertion: the task planner
requires specialist participants absent from the live registry. The persisted
risk assessment identified that prerequisite, rather than an LLM outage, as the
first block. This is not successful end-to-end execution.

The existing automatic Level 8 path skipped the entire required-participant
loop. Expanding it to approved Level 6, or accepting unchanged catalog membership
as specialist evidence, would bypass the framework contract. Independent review
identified this P1 issue. A temporary proposed exemption and its positive test
were discarded before acceptance; a local green fake-executor result for that
proposal is not proof of a safe implementation.

The final implementation removes the blanket exception. Approval, GET/HEAD
metadata, low risk, and `single_engine` cannot stand in for missing independent
review. All missing delegated participants are retained/deduplicated in
`riskAssessment.missingRequiredAgents`, with secret redaction. The review queue
names the prerequisite; Task Blueprint shows it, disables re-running, and asks
for replanning after correction. Existing final approval, authority, evidence,
resource and emergency-stop checks remain in place.

An existing test expecting empty-registry automatic execution was changed to
require refusal, reflecting the repaired security policy. Its lightweight
positive deterministic-runtime sibling remains, and new regressions cover both
automatic and approved missing participants plus ready participants. No fake
agent registration, fake independent review, fabricated model or waived live
acceptance was added to production code.

## Metadata foundation

The optional `ReadOnlyRuntimeInspector` reads the exact current stored API
configuration without execution or HTTP. It rejects missing/mismatched records,
mutable methods, scripts/runtimes, malformed URLs and bare targets (the existing
launcher defaults those to POST). Six tests, including forty target cases,
passed three race-enabled repetitions; task-package vet passed. The optional
interface is a metadata foundation, not a wired admission exemption or execution
grant. See `read-only-runtime-metadata-20261001.md`.

## Verification ledger

- Safety review: `approved-embedded-runtime-review-20261001.md`; the final
  fail-closed patch received no new findings in the bounded review. This is not
  a whole-system security certification.
- First integrated race run: workflow, automation and safety packages passed;
  task failed one old test expecting the unsafe empty-registry exemption. The
  failure log is retained in `runtime-safety-integrated-20261001.log`.
- Task Blueprint: `task-blueprint-prerequisites-run2-20261001.log` records
  **22 SUCCESS**, including run prevention and the actual missing-participant
  hint. An earlier TS7006 compiler failure was corrected and retained; it is not
  counted as a pass. Karma reported a slow headless-browser shutdown warning.
- The earlier production build succeeded at 703.22 KB initial / 115.30 KB
  estimated transfer with six SCSS budget warnings. It preceded the final UI
  gate. Its log is `frontend-production-final-20261001.log`; current final
  build/full-backend/browser results will be appended after terminal outcomes.

## Current integration findings and build evidence

The first full current backend run (`backend-final-20261001.jsonl`) completed,
but failed: 144 packages passed and three failed (frameworkregistry, pursuit,
source); 3,776 top-level tests passed and 66 failed. Test/subtest skip events
numbered 208, separate from two package skips. These are observed failures, not
production or release acceptance.

The failures exposed a shared redaction compatibility defect: harmless valid
JSON was reserialized, so callers comparing sanitized and original bytes
mistook legitimate requests for secret-bearing payloads. A separate source
adapter test exposed a query secret in a quoted Go HTTP error URL. Both require
fixes in the shared sanitizer, without weakening caller security checks.
The previous pure sanitizer results alone did not establish integration parity.

The final Task Blueprint production build passed in
`frontend-production-final-run2-20261001.log`: 703.22 KB initial bundle and
115.29 KB estimated transfer, with six existing SCSS budget warnings. This build
includes the missing-participant UI gate. Its 22 focused component tests passed;
it is not a current full frontend-suite result or operator completion proof.

The frontend Docker build now limits Angular workers to two by default, with
explicit allowed overrides of 1, 2, 4 or 8. It applies only to the build command,
after dependency installation, not the runtime. Five pure/static tests passed
both in the agent run and independently with the bundled Node runtime. No
CPU/memory benchmark or percentage resource saving is claimed. See
`frontend-build-resource-limit-20261001.md`.

## Subsequent integration repairs

The shared sanitizer now preserves exact benign JSON bytes, including duplicate
keys and already-masked values, without waiving size/depth or secret guards.
Gauss's compatibility report documents the source regression and race tests.
Its real frameworkregistry/pursuit runs still failed because the generic
sensitive-name policy also matched typed operational metadata.

Parent repairs use validation-only projections of exact private/operational
types: team identity and approval flag; portfolio numeric budgets, thresholds,
estimated usage and calibration usage; settlement execution destination.
All values remain scanned. Generic maps receive no exception. Original encoded
bytes remain the digest input, preserving approval/replay binding. The named
settlement payload retains the legacy field order and serialized names; a
regression asserts identical legacy serialization. Nested credential-bearing
text, generic sensitive fields, and changed numeric-budget digests are tested.
The general sensitive-key policy is unchanged.

Backend run 2 completed with 146 passing packages, one failing package,
3,844 top-level passes, five top-level failures, and 208 test/subtest skip
events. Its five failures exposed the settlement destination-label collision.
Source was being repaired during that run, so it is not immutable final-byte
verification. All failures remain recorded in `backend-final-run2-20261001.jsonl`.

Independent review additionally found broader URL-query and malformed-URL error
paths. The final sanitizer applies decoded sensitive-query policy to absolute
URL values and quoted URLs, including repeated values, encoded query names and
userinfo. URL-shaped parse failures are discarded; quoted errors use standard
JSON then Go string decoding, without discarding ordinary undecodable quotes.
Pure regressions cover those paths and source tests check returned, persisted
job and audit failures. No private accounts or successful remote call is used.

The reviewer reports no remaining blocking finding in this bounded final-source
review; this is not exhaustive log/privacy certification. See
`redaction-compatibility-independent-review-20261001.md`, retaining all earlier
findings and later dispositions. The parent task-admission race/count=3 run
passed (`task-final-safety-race-20261001.log`), covering automatic/approved
missing participants, ready participants and current read-only metadata.

Fresh final backend/race/vet, new owned-stack build and browser results are
pending terminal outcomes; earlier builds/runs must not be relabelled as them.

### Final-byte backend and CI results

`backend-final-run3-20261001.jsonl` completed with exit 0: **147 passing
packages, 3854 top-level test passes, zero test/package fail events**, and
**208 test/subtest skip events**, excluding package skips. This run follows the
final typed projection, decoded/malformed URL and Go-quote repairs; source
behavior did not change during it. Skip events do not prove optional database
or live-provider readiness.

The new build-resource regression command is wired into the existing frontend
CI job after dependency installation. `python scripts/test_ci_contract.py`
completed locally with 76 tests PASS; hosted execution is unverified. No new
dependencies, framework, service, permissions, credentials or paid capacity
were added.

## Isolation and release boundaries

### Current parent verification after the final backend repairs

The terminal `final-privacy-integration-race-20261001.log` passes three
race-enabled repetitions across safety, frameworkregistry, pursuit and source.
`backend-final-vet-20261001.log` ends with exit 0 and no diagnostics. These
checks supplement the 147-package/3854-test backend run above, not live accounts.

Synthetic owner `7428197162ac47be996715b3b261c634` was built from the final
backend source and reached healthy startup. Its retained browser run ends with
**11 PASS and 1 FAIL**. Read-only Basic route, authentication, disclosure,
navigation and error-recovery checks passed. The operator test completed a
persisted manual source job and same-source fixture extraction, pursuit intake
and exact runtime selection. It failed on the first run **before approval**:
the response was `blocked/blocked`, with no capable model selected, approval
required and framework-evidence preconditions unverified. It did not reach the
expected `needs_approval` state. Its `attempts: 1` is not proof that an external
tool executed. No current-image approval or terminal completion is claimed.
The earlier post-approval failure belongs to a different snapshot; these
outcomes must not be combined into an apparent successful journey.

Visual inspection then found long technical diagnostics dominating Basic view
and generated toolbar labels appearing below their button backgrounds.
Control Center now summarizes only the Basic presentation, preserves full
Advanced data and record navigation, and keeps inspection available during an
unrelated scan. Its 35 focused tests passed. Sources and Memory now use stable
icon/label rows; the shared subtitle selector targets only title subtitles,
not NG-Zorro button spans. The agent report records bounded static checks.
Fresh browser assertions require those labels to fit their buttons at all four
widths and HAI OS to render its loaded summary, not just a loading heading.

The complete final frontend rerun completed with exit 0, **1132 SUCCESS**,
Chrome Headless 151.0.7922.34, in `frontend-final-suite-20261001.log`. The
progressive-section source contract passed for 29 module templates, 146 static
sections and two dynamic prefixes; all ten proxy/landmark tests and the five
build-resource tests passed. Browser TypeScript checking passed. These are not
live-provider or complete populated Advanced action-chain acceptance.

The rebuilt UI browser results are pending in this subsection. The completed
earlier owned stack was removed with the guarded
Stop operation; its reports/private evidence remain, with no volume, source,
image or cache deletion. Fresh preparation uses owner
`8b65752c7f0845779ca3c4c77614af25`, manual-local mode and loopback port 58143.

### Rebuilt UI results

The new owner started healthy after the current UI production build passed:
703.22 KB initial bundle, 115.34 KB estimated transfer and six SCSS budget
warnings. Dependency/backend build layers were cached; the Angular build used
two workers and took 83.295 seconds on this busy host. This is a single build,
not a controlled performance comparison or a resource-saving percentage.

Its first complete 12-test browser run also ended **11 PASS/1 FAIL** in
`ui-final-browser.log`. All four 28-module Basic route iterations passed
(375/768/1024/1440px), now checking Sources/Memory label containment and a
loaded HAI OS summary. Authentication, local view persistence, direct sections,
skip navigation, mobile focus/inert/Escape restoration and dashboard error
recovery passed. The same real operator failure persists before approval;
the mandatory action-bound-proof and completed-result assertions were not
weakened or removed. Its selected workflow ID is
`c0308d2c-60f2-43d4-aa8b-2a0406d233e8` and task plan ID
`02420224-e1d6-4a74-8ffc-73ee6228e867`.

Directly viewed the new Sources mobile, loaded HAI OS mobile, Memory desktop
and blocked Command Center desktop captures. They confirm contained toolbar
labels, the loaded operating summary and a short Basic failure cause. A
subsequent 375px run with an explicit enabled-inspection assertion passed
(`blocked-inspection-browser.log`). The subsequent final Control Room rerun
completed with exit 0, **8 PASS and zero skips**, in
`inspection-final-browser.log`. It repeats the four route widths and the
disclosure/focus/error checks, explicitly checks enabled blocker inspection,
and verifies that clicking Inspect blocker opens the exact ID returned by the
real owner-scoped dashboard and its persisted blocked state. Non-read HTTP
requests were intercepted and aborted; no writes were observed during that
inspection. No mocked dashboard or fabricated record supplied that result.

Non-secret image identities are retained in the private test directory's
`ui-final-images.json`:

- Frontend: `b76f5d7dbcac360a8104cdd77bb72952b5ea38214d6a472d6b5804b50cb1e954`
- Backend: `cd046b775a9d4bc8e6eee153e84e9ea2b68c164387952b9cd1dd53f643dcabeb`
- IDP: `c68d7113cf45d30ce8b62505c7a6cbd61222ecefd3c344893f6c374f1a9b2dbf`

The retained evidence root is
`C:\Users\NO\AppData\Local\Temp\hai-acceptance-8b65752c7f0845779ca3c4c77614af25`.
Browser traces were disabled and process-only synthetic credentials restored
after each command. Do not publish the manifest, environment or configuration.
This verifies selected Basic/loading/recovery behavior, not every populated
Advanced action, contrast criterion, installation or live-provider journey.

### Final cleanup and repository state

The guarded Stop operation completed with exit 0 for owner
`8b65752c7f0845779ca3c4c77614af25`. A subsequent exact-label check returned
zero owned containers and zero owned networks. The browser log and private
manifest still exist; no files, evidence, volumes, images or caches were
deleted. Personal/shared installations and unrelated running resources were
not stopped or rebuilt. Agent work is integrated and the used agents closed;
all test/build commands reached terminal outcomes. Final scoped diff checking
passed. HEAD remains `e07b9daeb3ba2630ecbe12a64d948f791203c241`; no commit,
merge, push or personal deployment occurred. The existing dirty worktree was
preserved. The new UI is tested in temporary images, not installed into the
personal localhost stack.

The earlier nine owned containers and two empty owned networks were removed by
the guarded launcher, with ownership/project/mount checks, no volume deletion,
and all reports, files, images and caches retained. Docker returned no container
IDs for that exact owner afterwards. New testing uses only generated synthetic
owners, tmpfs databases, read-only source fixtures, a loopback gateway, and no
accounts/providers. Private manifests/configuration are not repository artifacts.

Required next work includes real framework-obligation implementation/review
evidence, current operator terminal completion, live-provider/OAuth/SMTP and
OpenClaw acceptance under explicit authority, OS sandbox attestation, signed
current-revision Windows packaging and clean-install/restore tests, complete
Advanced/accessibility action-chain verification, and hosted CI. Historical
tests, optional skips, registry labels and healthy containers do not close those
gates.
