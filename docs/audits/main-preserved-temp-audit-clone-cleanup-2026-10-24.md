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

## Follow-up standalone clone

The separate `codex-vercel-audit/018-HAI` temporary clone was subsequently
verified against the same remote main and preserved snapshot. All 3,244 tracked
files pass the same byte/line-ending recovery check; refs and reflogs contain
only the preserved snapshot, fsck is clean, and no ignored or untracked files or
shared worktrees are present. Hook samples were additionally compared byte for
byte with the installed Git templates; the exclusion file and repository
description contain no custom configuration.

After the native removal preflight rechecked remote preservation, ownership,
metadata, active consumers and working-tree state, only this explicit clone
directory was removed. Its parent directory was left intact. The removed target
contained 3,273 files and 48,168,530 logical bytes. Its absence was confirmed
afterward. C-drive free space increased from 6,196,875,264 to 6,252,978,176 bytes
during the operation; this observation is not a deduplicated allocation claim
or an attribution of all concurrent free-space changes to this removal.

A second candidate was excluded before any removal because it has 3,200
skip-worktree index entries. Its clean status is insufficient proof of local
file preservation. The earlier runtime-audit candidate also remains intact
because GitHub reports a commit outside main's ancestry.

Detailed follow-up receipts are retained separately from the original two-clone
receipts. These checks do not clear any other checkout, archive or database.
