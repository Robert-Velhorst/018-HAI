# Memory provenance recovery - 2026-10-20

The retained Go-source crosswalk identified `provenanceCanMerge` in an older
worktree. Inspection of main commit
`7f93a2229b6cfeb42a9ec1f18f1ae50a3b804017` confirmed that ordinary memory creation
could merge exact or similar text from different source URIs and retain only
the first source link. Existing stricter checks protected review-managed
verified facts, but did not cover ordinary project notes and preferences.

Service-level regression tests reproduced cross-source merging in the owner
and legacy paths, for both exact and similar content. A second test reproduced
the legacy hash shortcut merging unrelated content when its stored hash was
inconsistent with the canonical text.

The port restores source compatibility checks before exact and approximate
deduplication and validates canonical content in the legacy hash shortcut.
Different nonempty source URIs retain separate records. Repeated content with
the same source still deduplicates, and a missing source URI can still be
enriched. The verified-fact source/run-label rules remain in place.

A PostgreSQL integration test uses the existing isolated-schema fixture to
exercise the real repository and advisory-lock transaction path. The existing
PostgreSQL CI job runs all four owner/legacy and exact/similar cases and
requires explicit pass markers, so missing database configuration cannot
silently count as successful coverage. This fixture does not use application
or personal account data.

Local validation passed the memory, verification, and semantic suites and all
110 CI contract tests. The integration-tagged suite compiled without executing
database tests locally; PostgreSQL acceptance requires the CI pass markers.

The older implementation also merged unrelated text merely because its source
URI matched. That behavior is not restored: current content-similarity and
review-managed memory restrictions are retained.

This change prevents future provenance loss. It does not reconstruct source
links already lost in earlier merges, alter stored memory records, or establish
that the containing worktrees and transcript archives are ready for removal.
