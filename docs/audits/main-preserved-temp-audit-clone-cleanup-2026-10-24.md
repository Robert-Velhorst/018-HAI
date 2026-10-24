# Main-preserved temporary audit clone cleanup

Remote preservation target: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Preserved snapshot: `73999cf165a829a62f3d14c8ca7d8ead07afc491`.

## History and content evidence

Two standalone temporary HAI audit clones, identified by the leaf names
`codex-audit-018-hai-alerts` and `codex-audit-018-cleartext-20261020`, were
verified against the canonical repository before removal.

A fresh GitHub comparison proves that the snapshot is an ancestor of the
advertised main: the merge base equals the snapshot, candidate-unique commits
are zero and main is 100 commits ahead. This preserves the complete historical
snapshot on main's reachable history; it does not claim that every old file is
byte-identical to the latest main tree.

Each clone has 3,244 tracked files. A full content read compares each against
its committed Git blob: 111 are byte-identical and 3,133 are exactly recoverable
through ordinary LF/CRLF conversion. No other content difference is accepted.
All refs and reflog commits resolve only to the preserved snapshot. Full Git
fsck reports no errors or unreachable objects. No tracked changes, untracked or
ignored files, nonstandard index flags, extra worktrees or stashes are present.

The removal preflight also checks independent Git directories, standard metadata,
absence of custom hooks/unclassified local settings, root/descendant reparse
points, maintenance registration, active process consumers and container mounts.
Remote main, local HEAD, refs and working-tree state are rechecked before the
scoped native PowerShell removal. No parent temporary directory is removed.

## Removal result

Both named targets are absent after deletion. The first contained 3,274 files
and 48,168,779 logical bytes; the second 3,273 files and 48,168,677 bytes.
Together this removes 6,547 files and 96,337,456 logical bytes, including their
redundant Git metadata. Source and commit history remain recoverable from main.
Private preflight/content and post-removal receipts retain the detailed checks.

Other inspected audit copies are not automatically cleared. Some heads diverge
from main; others contain additional Git refs/reflog commits needing individual
preservation checks even when their checked-out HEAD is already an ancestor.
No such copy is removed based solely on a clean status or an ancestor HEAD.

The primary dirty checkout, integration checkout, eleven conversation archives,
unmerged source snapshot, installed toolchains and running applications remain
untouched by these two removals. This is bounded cleanup of fully preserved
copies, not completion of the whole-device integration/removal objective.
