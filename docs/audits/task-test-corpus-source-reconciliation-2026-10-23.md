# Task test corpus reconciliation

## Retained sources

The retained model-maintenance `backend/internal/task/service_test.go` is 85,704
bytes; the artifact-audit variant is 83,476 bytes. Their complete contents are
identical after CRLF-to-LF normalization. Both were checked against the current
published test file at `13933b0a744b37f671d39ac2651767b10845e3d1`.

The same Go AST/token method described in the workflow repository review accounts
for every declaration, retaining comments and literals. Each retained variant
has 120 declarations: 116 are token-identical, three changed, and the import
group is replaced by one retaining every existing import plus `lifeontology`.
No retained function is absent from the current file. The current file has 126
declarations. The full 137-added/3-removed-line diff and changed test/fixture
contexts were manually read; token-identical bodies were not all manually reread.

## Preserve current differences

- Brain-skill guidance calls now pass the explicit owner identity. Their
  deduplicated audit-pin and non-disclosure assertions remain unchanged.
- The nil runtime-result case now requires uncertainty, no retry availability,
  and failed validation. Exact diagnostic equality was intentionally relaxed to
  substring containment, allowing contextual explanation; unchanged diagnostic
  string strictness is not claimed.
- Additional tests assert that stored memory and life records remain contextual
  data, not instructions or action authority, including the outgoing provider
  prompt assembled by a local test harness.
- Additional coverage rejects a malformed request owner before brain-skill
  selection even when the plan's owner field is normalized.
- Additional retry checks distinguish untrusted-provenance rejection from
  missing supporting evidence. They test the current deterministic policy, not
  the correctness of an external model's judgment.
- Owner-scoped source and context-aware memory fake methods satisfy the newer
  interfaces and expose the retrieval context to tests. They are not real
  connectors or evidence that cancellation reached a provider.

No older test file was copied over the current one. No current test code was
edited by this review, and no dependency installation was needed.

## Verification and limits

`go test -p 1 ./internal/task -count=1` passed on the current published file.
The tests use existing local fixtures and mock provider/runtime boundaries.
They do not establish that real LLMs obey prompt-injection instructions, that
external runtime actions are safe, or that real connected accounts work.

Other retained task service, source, workflow, router and frontend variants remain
separate review work. Exact-head CI, publication, preservation on `main` and local
deletion remain independent gates. Both original test files remain intact.
