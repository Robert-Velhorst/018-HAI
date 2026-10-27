# Mini-SWE diff review integrity

The agent writes untrusted files in a disposable workspace. Previously, a path
present in the baseline but replaced with a workspace symlink could reach the
diff reader through the union of baseline and result names. `Path.read_bytes()`
would follow that link, despite the directory walk excluding symlinks. The
reader also checked file size only after reading the entire file. The agent
could modify the sibling baseline copy before the comparison read it.

The production reader now opens every directory component relative to an owned
descriptor with `O_DIRECTORY | O_NOFOLLOW`, and opens the final file with
`O_NOFOLLOW | O_NONBLOCK`. It requires a singly linked regular file and reads
at most the original one-file limit plus one byte. Missing result files still
represent deletions. Unsafe links, file types and unavailable platform
primitives fail the request. This requires the Linux/POSIX runtime; a weaker
path-based fallback is not provided.

The controller captures the baseline text before starting the untrusted agent
and uses that in-memory comparison afterward. The existing two-argument diff
API remains available. Original file-count, source-size and output-diff limits
remain; review file collections are additionally bounded before comparison.
Existing binary/oversize placeholder behavior is unchanged.

The hosted workflow runs all original Mini-SWE application cases with their
original bytes, assertions, helpers and fixture setup. No new application cases
are added. Source preservation, AST parsing and whitespace checks are local;
application execution occurs only on a disposable GitHub-hosted worker. The
existing suite does not independently establish adversarial filesystem-race,
baseline-tampering or full process/network isolation acceptance.

This is an incremental production fix. It does not resolve the agent's broader
execution boundary or establish closure of CodeQL alert 41. An authenticated
agent can still execute task-generated commands in its container; same-user
process visibility, surviving descendants, full filesystem/network isolation,
model authorization and deployment acceptance remain separate open work. No
alert is dismissed, no source excluded and no SDK/runtime version changed.

Primary references: [Python descriptor-relative file operations and flags](https://docs.python.org/3/library/os.html#os.open)
and [regular-file classification](https://docs.python.org/3/library/stat.html#stat.S_ISREG).
