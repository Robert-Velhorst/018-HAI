# Pursuits stylesheet source reconciliation

The complete retained stylesheet (2,579 lines, 53,171 raw bytes) and complete
872-line diff to the integration stylesheet were read. No older stylesheet was
copied over the current module: its retained layout/status/focus declarations
are already present, alongside newer project-dossier and runtime-stop recovery
styles. This round changes tests and documentation, not rendered styles.

## Comparison evidence

Both files compile in memory using the existing Angular build-owned Sass
dependency. PostCSS parses the emitted CSS, and the existing css-what parser
expands `:is()` alternatives structurally. Context/selector/property/value/
importance facts are then compared without rewriting either source.

| Scope | Retained | Integration |
| --- | ---: | ---: |
| Compiled rule blocks | 399 | 420 |
| Distinct declaration facts | 1,613 | 1,727 |
| Emitted CSS bytes | 50,547 | 51,992 |

All 1,613 retained declaration facts occur in the current stylesheet, with 114
additional facts. These are presence comparisons, not proof of source order,
specificity, inheritance, computed layout, contrast or rendered equivalence.
Selector grouping and shared-rule relocation were reviewed in the diff; for
example, the link-manager and linked-records template sections are separate,
so their differently located detail rules must not be assumed to overlap.

## Regressions guarded

Seven source-level tests protect:

- Structural selector expansion and media context.
- Theme tokens and keyboard-focus declarations.
- Distinct error and runtime-recovery declarations.
- The existing narrow-width control/grid declarations.
- Project dossier wrapping and responsive declarations.
- Workflow receipt caution and stale-reservation boundaries.
- Negative controls which remove focus or change a breakpoint in memory,
  proving these declaration checks can detect those losses.

The tests use existing build-owned Sass, Beasties/PostCSS and css-select/
css-what dependencies through their owning package resolution paths. They do
not install dependencies, generate a build directory or load a browser.
The existing control-room test entry imports the new tests, so the current
hosted native test command also runs them.

## Limits And Retention

This does not establish that a page is visually correct at any viewport, that
its actions reach a real provider, or that all related templates/controllers
have completed reconciliation. The large retained Pursuits template/controller
and their other variants remain separate review items. The original stylesheet
and every other selected source remain retained. Publication, CI acceptance,
verified preservation on main and deletion eligibility are separate gates.
