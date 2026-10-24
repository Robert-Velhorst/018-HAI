# Main-preserved temporary audit clone cleanup

## Runtime-pin review copy

The independent `018-hai-latest-stable-audit` clone was removed only after its
single historical proposal commit `de415ca4c489254ba068dfb46563d7d56631efa0`
was preserved and reverified under annotated GitHub tag
`archive/local-preservation-20261024/hai-runtime-pin-review`. Its parent
`2fac81caee9f5005d95e72a0f4e9b5b4ca6cc3c8` is an ancestor of reviewed main.
The intended runtime pin and targeted test are already retained on main;
later Go builder pins and unrelated subsequent test improvements were not
reverted. This is not an assertion that every historical file equals the
latest main tree or that the archived commit itself was merged to main.

The one-commit range passed the pinned Gitleaks scan. The archived workflow
allows push events only for main, not the archive tag. No deployment or
application stack was started by this operation.

All 3,231 checked-out tracked files were read against the archived Git blobs:
110 are exact and 3,121 differ only by normal line endings. No indexed file
was missing. Refs and reflogs contained only the archived head and its preserved
parent. Git fsck was clean, and no untracked/ignored files, extra worktrees,
custom hooks or unpublished commit-message draft were found. Physical source
and metadata hashes were rechecked immediately before native PowerShell
removal, along with exact path resolution, reparse points, maintenance entries,
process command lines and running-container mounts.

The explicit clone contained 3,277 files including Git metadata and 47,845,902
logical bytes. Its absence and the unchanged remote main/archive tag were
confirmed after deletion. Observed C-drive free space changed from
6,909,181,952 to 6,964,404,224 bytes; this is not attribution of all concurrent
free-space changes to the removal. Original source, integration checkout,
databases, recovery artifacts and conversation archives remain intact.

## Subsequent sparse main-ancestor audit copy

The previously excluded `codex-018-hai-audit-20261020` copy was later reviewed
with explicit sparse-checkout handling. Its head is the preserved main-ancestor
snapshot below, with zero candidate-unique commits. All 44 physically present
tracked files were read and matched their committed blobs after ordinary
line-ending normalization. The other 3,200 indexed paths were absent under the
reviewed sparse selection; they were not counted as verified physical files.
The index matched HEAD, refs and reflogs contained no additional history, fsck
was clean, and no ignored/untracked files or shared worktrees were found.

Sparse configuration and source hashes were frozen and rechecked by the native
removal preflight, alongside exact target resolution, metadata classification,
reparse points, maintenance registrations and process/container consumers.
Only that explicit clone was deleted. Its absence was confirmed afterward.
The removed target contained 84 total files and 2,625,850 logical bytes.
Observed C-drive free space changed from 7,127,756,800 to 7,130,517,504 bytes;
concurrent filesystem changes prevent attributing every free byte to this action.
The original working checkout, integration checkout and transcript archives
were untouched. This resolves the earlier sparse-copy exclusion for this exact
target only, not any other checkout or the full cleanup objective.

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

## Follow-up fleet audit clone

The HAI-only child of the `codex-github-fleet-ngrok-20261019` temporary directory
was checked against preserved snapshot
`21ad54689f6ed9a78714ee433e8573461e6ea95a`. GitHub confirms that this snapshot is
an ancestor of the same reviewed main with zero candidate-unique commits.
Its 3,233 tracked files pass the committed-blob recovery check: 111 exact and
3,122 ordinary line-ending conversions. No extra refs, reflog heads, untracked
or ignored files, custom hooks, hidden index flags or shared worktrees were
found. Git fsck reports no unaccounted local objects or errors within the
partial clone; it does not claim that every promised remote blob was locally
downloaded. All checked-out file bytes were read and verified.

The first native removal preflight stopped before deletion because its metadata
classifier did not yet recognize partial-clone settings. Inspection identified
only `remote.origin.promisor=true` and `remote.origin.partialclonefilter=blob:none`.
The explicit batch accepts those two exact values only; any other unclassified
configuration still refuses removal. No policy-blocked deletion was retried and
no alternative deletion mechanism or parent-directory removal was used.

After fresh native preflight checks, the explicit HAI child directory was
removed and confirmed absent. Its parent remained. The removed copy contained
3,267 files and 48,143,291 logical bytes. Free space observed immediately before
and after was 6,250,446,848 and 6,305,902,592 bytes; this is not a claim that all
concurrent free-space changes came from this removal.

The two follow-up removals together account for 6,540 files and 96,311,821 logical
bytes. Other divergent, sparse, dirty, shared or incompletely reconciled copies
remain intact. This is not completion of the full local-data removal objective.

## Sparse nginx audit copy

The `codex-018-nginx-audit` copy used a sparse, partial checkout at
`e79bdf85663da012aa1c4c733e9bd414a8a88d04`. This commit is not an ancestor of
main. Its parent is the preserved main-ancestor snapshot above, and all seven
of its nginx pin replacements are already present on reviewed main. A
zero-context, LF-normalized reverse patch check and structured comparisons of
the three gateway images and frontend runtime stage verified that narrow
source disposition. This does not assert equality of the complete old and
current-main trees.

The original commit was separately published and verified under annotated tag
`archive/local-preservation-20261024/hai-nginx-pin-review` before removal. Its
one-commit change range passed the pinned Gitleaks scan without rule changes.
Missing objects needed for publication were read from existing retained local
object stores; lazy downloads stayed disabled. Main was not changed by the tag.

Sparse-aware content verification checked all 164 physically present tracked
files, including any present skip-worktree files, against archived HEAD blobs.
Seventeen were exact and 147 ordinary line-ending conversions. The other 3,080
indexed skip-worktree paths were absent on disk, not silently accepted as
verified present content. The index itself matched HEAD. A physical source-file
walk rejected files outside the verified set. The sparse selection and its two
worktree flags were inspected explicitly; the commit-message draft matched the
published commit message. Private settings or an unpublished draft would refuse
removal.

Git fsck returned no errors but listed nine unreachable trees/blobs. Each object
was individually found in the exact parent snapshot retained in main's history;
none was an unreviewed commit. Thus this was accounted unreachable data, not a
claim of zero unreachable objects.

The native removal preflight rechecked the remote archive, main ancestry of the
parent, refs, consumers, mounts, reparse points and source state. It rehashed
every present source file and the index, local/worktree configuration, sparse
selection and commit-message metadata immediately before deletion. A newly
appearing skip-worktree file or altered source/metadata would refuse removal.

Only the explicit sparse audit directory was removed: 219 physical files,
3,385,153 logical bytes including Git metadata. Its absence was confirmed.
Observed free space changed from 6,282,039,296 to 6,285,860,864 bytes; concurrent
free-space changes are not attributed wholesale to this removal. No transcript,
database, backup, original dirty checkout or runtime was deleted by this step.
