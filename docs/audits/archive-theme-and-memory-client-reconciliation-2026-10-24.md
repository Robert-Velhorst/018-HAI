# Archived theme and memory-client reconciliation

Baseline: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Three historical variants are covered: `services/theme.service.ts`
(`536bb5bf445d`), `services/context-memory.service.interface.ts`
(`8f14c10e06d4`) and `services/context-memory/context-memory.service.ts`
(`fdcbc3e621e0`), relative to `frontend/src/app/`. These are overlapping parts
of the existing TypeScript cohort, not newly discovered source files.

## Theme source disposition

The historical light-first default has deliberately become dark-first, as
specified for the control-room redesign. Both saved preference keys remain
readable, explicit light remains valid, and the current key wins when the two
keys disagree. Invalid saved values or unavailable storage fall back to dark.
Setting a mode updates the root/body theme classes and body color scheme;
changes are observable through a BehaviorSubject. Both keys remain written for
compatibility. The dark-mode icon name changes from `moon` to `star`; this
source comparison does not independently verify that every icon is rendered.

## Memory-client source disposition

The historical create request remains. Record update, archive, restore and
delete keep their HTTP verbs but encode IDs as single URL components. The list
method retains its project/archive arguments and adds an optional bounded
limit. The new query operation forwards only a defined filter list, excluding
caller-supplied owner identity. The server must still enforce ownership.

The interface exposes query/page types and semantic reindexing. `export` is
renamed to `exportMemories` without changing the project-filtered export
transport. Retrieval normalizes missing/null `usedContext` to an empty array
while preserving existing context. These changes must not be reverted by
overwriting current source with the historical snapshot.

## Focused current-main checks

All 25 local checks pass using current-main source transpiled by the installed
TypeScript compiler. Archived source is compared as data, never executed.
The checks use the installed real RxJS implementation, an immutable HttpParams
test double, a recording HTTP double returning local observables, and in-memory
DOM/localStorage doubles. They do not instantiate Angular TestBed.

Ten theme checks cover first-run dark, explicit light/dark, legacy light/dark,
current-key precedence, corrupt storage fallback, blocked reads, toggle
notifications plus persistence to both keys, and blocked writes still allowing
the selected mode to apply. Construction checks also verify root/body class
exclusivity, the root theme attribute and body color scheme.

Fifteen memory checks cover default list transport; five limit cases (zero,
above maximum, NaN, infinity and a fractional value); query defaults/clamping,
trimmed filters and owner exclusion; encoded update/archive/restore/delete IDs;
missing context normalization; existing context retention; bounded semantic
reindexing; and the renamed export's project filter. No HTTP request, archive,
restore, delete or reindex operation reaches a real server.

A negative control removes the first `encodeURIComponent(id)` only from the
in-memory current-source copy. The encoded-update URL assertion then fails as
expected. No product file is changed. This detects a transport regression; it
does not prove backend authorization, input validation or URL routing behavior.

## Limits and preservation boundary

There is no full type check, Angular template compilation, browser rendering,
real localStorage/DOM, live API or database acceptance in this report. In
particular, theme propagation through every page and overlay still needs browser
validation, and backend memory visibility remains a separate policy boundary.
Malformed response shapes beyond the tested cases are not certified.

This source/contract review does not clear entire files, conversations or
archives for deletion. Complete requirement integration, publication, exact-head
CI and current-main preservation remain open gates. Source deletion eligibility
is false for this receipt.
