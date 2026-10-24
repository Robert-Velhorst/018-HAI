# Provider and Windows release documentation reconciliation

## Provider review

Read the retained provider review completely and its full current difference.
Preserve distinctions between implemented adapters, historical bounded evidence
and current account-specific acceptance. Keep current incremental Trello action
sync, complete card inventory, signed webhook intake prerequisites, lack of a
provider write path and the disabled/unimplemented Autofever connector. This
review does not claim a new live source/provider acceptance run or complete
review of the large connector implementation.

Both retained and current documents incorrectly claimed source rows never store
board IDs. The current source-creation path normalizes Trello `SyncTarget` with
`trelloBoardID` and persists it in `ConnectedSource`. The REST adapter reads
that field for board requests. Correct the privacy statement: API credentials
are distinct from persisted board/source identifiers and provenance. Do not
tell operators that a diagnostic or backup containing source metadata is free
of provider identifiers. The correction links to the implementation rather than
inventing live evidence or a new data-retention guarantee.

## Windows installer review

Read the retained installer guide completely and the complete current difference.
Keep newer unsigned-preview versus signed-production-candidate distinctions,
explicit signing inputs, dirty/skip-compile rejection, data-preserving recovery,
separate readiness checks and legacy-process cleanup wording. The installer
support helper rejects host-runtime enablement unconditionally; do not restore
the older enablement instructions or claims that process success verifies work.
Keep DSH hard-disabled until its channel, sandbox, start/cancellation and host
acceptance gates are satisfied.

Inspect the current signing parameter/policy guards, environment-before-worker
startup order and unconditional DSH block. Run the read-only environment-only
recovery contract and verify document-relative links. These checks do not build,
sign or install an executable, inspect a certificate, start Docker, register
tasks, recover an environment or establish clean-machine Windows acceptance.

The read-only environment-only recovery contract passed. All five relative
links in the two reconciled documents resolve to existing repository files.
`git diff --check` passed. No installer compile, signing or live-provider test
was performed for this documentation change.

## Preservation boundary

No runtime setting, credential, account, database or installed worker changes.
Original retained files remain intact. Publication of this review is not proof
that every HAI artifact, archive or complete worktree can be deleted.
