# Prepared-history secret-scan preflight

Comparison base: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This records local preflight evidence, not GitHub CI acceptance, publication or
main integration.

## Scanner and scope

The existing cached source directory for CI's pinned
`github.com/zricethezav/gitleaks/v8@v8.30.1` was built locally using Go 1.27.3,
with proxy/sumdb network lookups disabled, a read-only module graph, two Go
threads and a dedicated build cache. No new Docker container, agent, dependency
download or application test environment was started. This is a local source
build, not an independently verified signed release binary.

The native scanner used its default rules, full-value redaction, a JSON report
captured privately, and the existing `.gitleaksignore` whose Git blob matches
the prepared tree. No rules, ignores or allowlists were added or relaxed.
The wrapper refuses an empty revision range, changed remote main or unexpected
scanner-configuration environment overrides. Raw scanner output is withheld.

## Results at the recorded checkpoints

The clean prepared head `b3366d8789f8` contains 147 commits after the comparison
base. The pinned native scanner exits zero with a valid JSON report containing
zero findings for that range.

The original prepared head `a20bd9fef01e`, also 147 commits after the same base,
is used as a positive control. It exits one and reports exactly the known
`generic-api-key` finding at line 28 of
`docs/audits/failed-archive-patch-reconciliation-2026-10-24.md`, introduced by
the existing published documentation commit. The candidate value is not copied
here. Earlier reconciliation established it as an archive-record fingerprint;
the sanitized series shortens the documentation references without weakening
the scanner. The original history therefore still fails; only the clean
checkpoint passes this preflight.

The positive control demonstrates that the same tool/configuration still
detects the original finding. It is not a proof that every possible secret or
personal-data disclosure can be detected automatically. Manual privacy review
and exact-head CI remain required.

## Resource and preservation boundary

The dedicated compiler cache created for this preflight was cleaned using
native `go clean -cache`: 182,272,685 logical bytes became 228 bytes. This is
removal of newly generated verification overhead, not a claim of equivalent
net user-data storage recovery. Existing module sources, installed toolchains,
application code, databases and transcripts were not removed.

The scoped force-with-lease rewrite of PR #319 remains unapproved, as does its
merge. The separate Promptfoo dependency-audit failure is not resolved or waived
by a clean secret scan. Later commits need a fresh scan; these exact-checkpoint
results must not be silently extended to a different head. Archive and source
deletion eligibility remains false.
