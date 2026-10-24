# Local Ngrok and Go review-history preservation

This preserves existing local history from a separate temporary clone. It does
not publish, rewrite or merge PR #319, and it is not a deployment.

## Discovered history

The temporary Vercel-audit clone also owns the Git metadata of a second Go
verification worktree. Its checked-out main snapshot is already reachable from
current main, but the repository additionally contains local review history,
reflog entries and an older consolidation tag. A HEAD-only deletion check would
have missed those dependencies. Both working copies remain intact.

Five local commits after `73999cf165a829a62f3d14c8ca7d8ead07afc491` culminate
at `0965019abdd11b44ff83c676fb52770359c655da`. They contain Ngrok retirement,
retirement-CI repairs, opt-out environment migration cleanup, Go 1.27.3 tooling
and digest-pinned builder test support. The net patch touches 48 paths. This
history was not initially proven accessible as a remote commit reference.

The three digest-aware test patterns and the three PostgreSQL preflight
`command: ["postgres"]` settings inspected during this review already exist
in current main. Those targeted matches are not proof that every change in the
five-commit series is fully integrated. An older PostgreSQL consolidation tag
was separately verified to exist remotely with its expected peeled commit.

## Publication and exact remote verification

The five-commit range passed the locally built CI-pinned Gitleaks v8.30.1 scan
with zero findings. Added source/configuration changes were reviewed before
publication. No private transcript, local credential file or test output was
added to this history. The source revision contains only the CI workflow, with
pushes restricted to `main` and pull requests; the workflow trigger structure
was parsed using the installed YAML BaseLoader before publishing the tag.

The existing history is now preserved in the canonical HAI GitHub repository as
the annotated tag:

`archive/local-preservation-20261024/hai-ngrok-go-review`

Remote tag object `cef8730315b78b1d30e82d7a901805c64635161d` and peeled target
`0965019abdd11b44ff83c676fb52770359c655da` were verified with `git ls-remote`.
No existing tag was replaced, no force push was used, no branch was created and
the advertised main commit remained unchanged. The archival tag has no configured
tag-push CI trigger in its source revision; no workflow was explicitly dispatched.
It is not a release or a claim of live runtime acceptance.

## Cleanup boundary

The full history is now remotely recoverable independently of the local clone.
That is publication/preservation progress, not proof of main integration. The
shared clone and Go worktree stay until their remaining main-preservation and
ownership checks are complete. PR #319 rewrite/CI approval and PR #332 merge
approval remain separate, pending decisions. No local source copy is removed
by this publication step.
