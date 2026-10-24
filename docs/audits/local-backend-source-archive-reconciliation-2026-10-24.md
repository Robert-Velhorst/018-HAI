# Local backend source-archive reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This concerns two source-build TAR snapshots, separate from the eleven retained
conversation archives and their patch-index cohort. Counts must not be added
to that cohort as newly discovered historical patch variants.

## Complete read-only member comparison

Each TAR is 23,326,720 bytes and contains 2,141 regular files under `backend/`
and 160 directories. Both were parsed without extraction or source execution,
with bounded entry/file sizes and rejection of unsafe paths, duplicate files
and unsupported link/special members. Source size, timestamp and file identity
were checked for stability. Member contents were compared against Git blobs,
not a potentially dirty checkout.

The B4 snapshot has one byte-identical file and 2,140 line-ending-equivalent
files on main, with no missing or other changed contents. A second, stricter
check reconstructs every original file's exact bytes from main using only the
recorded line-ending conversion. All executable bits match main and there are
no extra empty directories. This proves preservation of source bytes and Git
executable semantics, not equality of the TAR container's timestamps/headers.

The E0F snapshot has one byte-identical file, 2,136 line-ending-equivalent files
and four other changed files on main. Those four are `backend/go.mod`,
`backend/go.sum`, `backend/internal/infra/database_pool.go` and its test file.
The changed Go pool source encodes query spaces as `%20` instead of form-style
`+`; dependencies and accompanying tests also change. None was blindly overlaid
onto the prepared backend, which already has separate source repairs.

## Mapping existing published work

All four E0F differences match the corresponding contents at PR #332 head
`30d895fdd572fa4e46628639f36ff9a05f265d69`, with only CR-byte/line-ending
differences. GitHub API content was read at that exact revision; member bodies
were not published by this audit.

At inspection, PR #332 is open, draft and cleanly mergeable, with all 41 reported
exact-head checks completed successfully. These include backend/frontend tests,
browser acceptance, PostgreSQL migration integration and isolation checks.
Those are GitHub check results, not fresh local runs or external-account/live
provider acceptance. The PR is not merged by this review. Permission to mark it
ready and squash-merge has been requested separately.

Seven older user-supplied ZIP leaf names were also checked at their originally
provided Downloads locations and were absent there. Absence at those locations
does not prove safe prior deletion, complete integration or absence elsewhere.

## Scoped B4 removal

Only after fresh remote-main verification, full-file reconstruction/executable
checks, reparse-ancestry checks, consumer/mount checks and a new full-archive
hash check, the B4 source copy was removed with native PowerShell. An open read
handle prevented concurrent readers/writers during verification and permitted
the scoped deletion. The final absence check passed; 23,326,720 logical bytes
were removed. TAR header metadata was not retained as product source.

The E0F snapshot remains because its four differences are published in a PR,
not yet preserved on main. All eleven conversation archives remain. Neither
an open PR, passing CI nor this member comparison clears those other sources
for deletion. The remaining integration objective is not complete.
