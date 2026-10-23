# Original-checkout inventory checkpoint

## Broader scope remains open

Completing the selected 618 retained worktree variants does not complete the
original HAI checkout, ignored/baseline contents, recovery clones, or transcript
archives. This checkpoint starts a separate inventory of the original checkout's
changed and untracked paths; it does not replace that larger goal.

The original checkout was inspected with Git optional locks disabled. Its status
was stable across the read:

- 692 changed tracked paths: 244 present, 448 already missing.
- 1,578 untracked paths, including 1,498 under `output`.
- 1,806 regular files were content-hashed with a 2 MiB per-file limit; 16 present
  entries exceeded that bounded hashing scope or were otherwise not hashed.
- Present listed entries total 136,962,592 logical bytes. This is not the size of
  the complete checkout, Git history, or transcript archives.

The cached `origin/main` reference matched the freshly queried remote main at
inventory time. Within these hashed changed/untracked paths, no files matched its
Git blob byte-for-byte or by CRLF-to-LF conversion alone. This says nothing about
unchanged files, which are outside this status inventory. Eleven listed paths
match an exact path/content variant in the previous selected-source reviews;
that overlap is a review lead, not whole-checkout preservation proof.

Existing missing tracked paths are not new deletions performed by this audit.
They are not staged or published as intentional removals. Untracked output is
not bulk-published: it still needs classification for relevance, privacy, and
useful implementation evidence. Ignored files and files above the hashing bound
remain explicitly outside this checkpoint's content verification.

## First original-source comparisons

`backend/internal/accountfeed/permission_registry.go` was read in full alongside
its complete four-added/three-removed-line diff. Its fields, constructors, lookup,
declared read-only scopes, and unconditional `WriteAllowed=false` remain in the
current implementation.

Current `main` adds the `ReportsCredentialGrant` requirement: OAuth client setup
and legacy token presence are not represented as owner grants. Relevant bridge
definitions and existing Google grant regression assertions were read. The
normalized-feed registry is not advertised as the live provider connector;
provider-native health belongs to Connected Sources.

This file needs no redundant port from the original copy. Its raw original
content still differs from `main`, so it is not marked deletion-eligible. The
comparison does not certify the original bridge implementation, other files,
live provider access, or fresh test execution. The inventory's Git blob framing
was checked against Git's own `hash-object` on this representative source.

The complete original `backend/internal/accountfeed/bridge.go` and its original
27-added/six-removed-line delta were also read. The existing provider constants,
parsing/lookup functions, local-feed availability, assisted/import contracts,
scopes, and read-only limits remain. Main adds an owner-bound read-only Autoposter
adapter and credential-environment names without promoting configuration into a
verified provider read. Relevant current Autoposter validation/configuration/GET
contexts were inspected; no account configuration or network read was performed.

Two comments still described the registry as having no API adapter at all. They
now distinguish metadata-only Gmail/Drive/Calendar/GitHub/Trello contracts from
the opted-in Autoposter adapter. This is a comment-only correction, not a new
connector implementation or claim of live acceptance. The complete original
Autoposter-adjacent dependency graph and the remaining original source files are
not certified by these two file reviews. No weaker original grant behavior is
restored and neither original file is marked deletion-eligible.

## Runtime handler comparison

The complete original-to-current diff of
`backend/internal/agentruntime/handler.go` contains 33 added lines and one
replaced line. The additions check request cancellation after upload inspection,
before archive persistence, and immediately before the selection mutation under
the execution commit fence. The original callback dispatch is replaced by that
guarded dispatch; restoring the older handler would remove these checks.

The upload authorization, archive persistence/validation and selection contexts,
the adapter's commit fence, and the existing cancellation regression tests were
inspected. The fence takes the adapter inventory lock before the execution fence,
checks the emergency stop, and then invokes the guarded callback. Existing tests
cover cancellation before upload authorization, cancellation after approval,
cancellation during fence evaluation, and preserving an error returned after the
callback starts. This checkpoint inspected those tests; it did not rerun Go tests
or exercise a live OpenClaw installation.

This is a comparison of the retained file and its changed execution boundaries,
not a full runtime security certification. Archive persistence precedes selection
commit: cancellation after persistence is not proof that no disk write occurred,
and the checks do not promise rollback or interruption inside a started callback.
The current handler's Git blob matches the inspected `main` reference. The raw
original is different, so neither it nor the original checkout is released for
deletion under the direct-match rule.

## Storage boundary

No original sources, Git history, databases, or transcript archives were deleted.
An attempt to remove only the disposable Node compile cache generated by these
test runs was blocked by execution policy before the command started. The cache
was confirmed still present; no alternate deletion route was attempted.
