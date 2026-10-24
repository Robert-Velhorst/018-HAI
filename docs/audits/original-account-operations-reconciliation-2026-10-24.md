# Original account-feed and operations reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Prepared checkpoint: `9d0c22b9da4803140ddb9004febc286857eca6d5`.

## Source differences reviewed

Six modified original files were compared without editing the original checkout:

- `accountfeed/bridge.go` and `accountfeed/permission_registry.go`.
- `operations/domain.go` and `operations/service.go`.
- `opscontrol/handler.go` and `opscontrol/service.go`.

Their raw hashes still match the frozen source inventory. The newer behavior
described below is already represented on reviewed main; no older implementation
needs to replace it.

### Provider capability and grant truthfulness

The bridge registry distinguishes normalized import contracts from native,
owner-scoped Connected Sources health. Configured Gmail/Drive/Calendar OAuth
client bootstrap is not treated as a granted owner credential. Permission
projection requires both the credential-presence status and a bridge type that
actually reports a direct credential grant.

Main also has the opted-in read-only Autoposter bridge absent from the old local
variant. Complete configuration can report credential presence, never a verified
read or execution authority. Manual/import providers remain contract-only.

### Operation evidence and reference ordering

Main preserves the original operation defaults and adds an internal raw-source
evidence SHA-256. Creation records it; refresh compares incoming raw evidence
against that fingerprint rather than comparing raw input to a potentially
redacted persisted representation. The model field uses `json:"-"`; it is not
public operation JSON. This review is not a live database migration check.

The `SourceSequence` intake field belongs to read-only reference synchronization,
whose separate validation/checkpoint path binds owner, workspace, feed and source
ordering. It is not a new execution-authority field or a claim that generic
operation creation itself persists a source sequence.

### Exact control review

Current source routes stop recovery through a durable owner-scoped review,
requires the exact resume confirmation, prepares server-derived control effects
and appends explicit approval decisions. It rejects malformed/noncanonical
request IDs and does not accept a caller-supplied proof as a substitute for the
review flow. Direct disengagement of an already-clear stop reports that no
change is required; the separate approved-review flow has an idempotency test.

Static route inspection finds the configured installation-principal guard on
resume-review and control-approval routes. That is source wiring evidence, not
end-to-end proof of the first-run account/bootstrap/JWT chain. No installation
account, global autonomy setting or actual emergency-stop state was changed.

## Executed checks

All 121 Go source/test files in accountfeed, operations and opscontrol plus both
module files match the retained prepared inputs. Compared with main, the only
selected-file difference is explanatory comment text in `accountfeed/bridge.go`;
the relevant executable code is retained. Full transitive-source equivalence
to either complete tree is not asserted.

The three native package suites passed: 376 root tests and 1,632 passing test
cases including subtests, with zero failures. One test was skipped:
`TestLocalImportRejectsLinksAcrossBothReaderPaths` could not create a symbolic
link because this Windows token lacks the required privilege. That scenario is
not accepted as tested. The separate native Windows-junction rejection test
executed and passed.

The run used Go 1.27.3, offline dependency resolution, read-only module files,
CGO disabled, one build worker, GOMAXPROCS=2 and a 512 MiB GOMEMLIMIT soft target.
Workspace/user Go flag overrides were disabled. Integration-tag tests were not
compiled or executed: no real PostgreSQL or external Autoposter acceptance is
claimed. Fixtures used owned temporary files and loopback HTTP services. No
container, installed HAI runtime, paid provider or external account was used.

All 123 retained package/module hashes and all six original hashes were stable
afterward. The owned temporary directory was empty. Native build-cache cleanup
reduced this check's 273,201,707 logical cache bytes to 228 bytes; this removes
verification overhead rather than proving equal net reclamation of old data.

## Remaining scope

This records the six observed source deltas and bounded native package evidence,
not full original-checkout or conversation-archive integration. It does not prove
browser behavior, live consent, provider connectivity, PostgreSQL behavior,
installation identity authorization, full CI or production acceptance. The raw
original files, ignored/untracked data, other worktrees and archives remain
separately scoped. No original source file or whole checkout was deleted.
