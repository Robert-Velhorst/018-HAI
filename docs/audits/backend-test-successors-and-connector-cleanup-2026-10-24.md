# Backend test successors and main-matched connector cleanup

Reviewed main: `f7bc8ae30aebfd114cc98ae18848aabf0829f32f`.
Prepared comparison head: `4317aab20e347e6f83ab71b5e8447084d80a7f52`.
This is a bounded continuation, independent of pending PR #319 CI. It does not
claim the remaining transcript archives or all local changes are integrated.

## Eight complete original test files reconciled

All original bytes still match the frozen source inventory. After ordinary
line-ending handling, each complete file equals both reviewed main and the
selected prepared head after only the following explicit transformations:

| File | Complete successor change |
| --- | --- |
| `backend/internal/automation/public_projection_test.go` | Two successful-update cases use the accepting event publisher; other publisher cases remain unchanged. |
| `backend/internal/executionauth/transactional_test.go` | One task-review fixture samples its fixed clock once, then uses that timestamp consistently. |
| `backend/internal/llm/outbound_transport_test.go` | The TLS fixture sets the server's pre-StartTLS `TLS` field. |
| `backend/internal/opscontrol/emergency_stop_fanout_test.go` | The pause request supplies its required JSON reason and content type. |
| `backend/internal/router/opscontrol_emergency_stop_permissions_test.go` | The permission test supplies the same required JSON reason without changing its authorization assertions. |
| `backend/internal/verification/freshness_test.go` | The status calculation supplies grounded mode and its request to the current function signature. |
| `backend/migrations/protected_data_rollback_postgres_integration_test.go` | The synthetic migration filesystem includes its empty post-phase directory. |
| `backend/migrations/runtime_db_role_provisioning_contract_test.go` | Script content normalizes CRLF before testing command ordering. |

All thirty original root tests remain in the same order. Eight negative controls
replace a failing assertion with a log assertion; every altered successor is
rejected by the complete-content comparator. The first comparator draft used
an overly broad publisher replacement and omitted an import; both stopped
without modifying source and were narrowed to the complete actual differences.
No Go, database or browser test was executed by this static review. These
files need no old-code restoration; they are already represented on main.
The other seventeen files in the original 25-file backend test cohort remain
outside this full-content disposition.

## File-only connector cleanup

The expanded 3,202-path connector inventory separately identified 2,854
physical files already matching current main. Every original file's exact
bytes were reconstructed from its main blob using only exact equality or one
ordinary uniform line-ending conversion. Missing blobs, mixed conversions,
alternate data streams and changed source hashes are not waived.

After a fresh remote-main check, path/reparse checks, process checks and
metadata/source integrity checks, only those 2,854 files were removed using
native file-by-file deletion. Each file was hashed again under a handle that
denied concurrent writers before deletion. No recursive directory removal,
Git index staging, checkout restoration or source execution was performed.

The Docker desktop endpoint was unavailable. Its exact local named-pipe
context, absence of Docker backend processes and absence of running WSL
distributions established an inactive local container runtime; nothing was
started to conduct cleanup. This is not a healthy-daemon or live application
acceptance claim.

The cleanup removed 25,996,654 logical bytes. Observed C: free space increased
from 10,576,826,368 to 10,613,604,352 bytes; unrelated filesystem activity can
affect that measurement. The 348 remaining cohort files, all 237 shared Git
metadata files and the linked worktree pointer were checked byte-for-byte
after removal. Registered worktrees remain unchanged. Ignored files, sibling
worktrees, databases, backups and transcript archives were not removed.

The connector checkout now intentionally contains tracked working-file
deletions. Do not stage or commit those cleanup deletions or restore the old
files during later source reconciliation. This is not whole-checkout deletion
clearance; unique source and shared history still require their own review.
