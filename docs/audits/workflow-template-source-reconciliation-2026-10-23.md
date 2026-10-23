# Workflow template source reconciliation

The retained 63,290-byte workflow template was compared against published commit
`13933b0a744b37f671d39ac2651767b10845e3d1`. Both complete templates were parsed
with the existing Angular compiler, including structural templates. All retained
event-handler expressions and named form controls remain present. The current
template has 67 buttons versus 61 retained buttons; all explicitly use
`type="button"`. A set comparison does not establish handler multiplicity,
rendered visibility, backend wiring or action safety.

The complete 130-added/57-removed-line diff was read. Preserve the current
template rather than restore the older snapshot:

- Uncertain worker outcomes require refreshed records and explicit review.
  Reminder authorization and worker controls retain the corresponding gates.
- Shared busy/loaded/error guards, per-record opening progress, checklist errors
  and affected-workflow refresh controls remain available.
- Approval controls require the pending approval state, not merely the absence
  of a final status. Recovery asks for prior-outcome reconciliation explicitly.
- Intake retains input length/validation feedback, source provenance, optional
  success criteria and links to the exact returned project/workflow.
- Search stays outside the filtered-list visibility condition, so a zero-match
  search does not remove its own control. The page no longer nests a second
  `main` landmark inside the shared shell.
- Decorative nonfunctional attachment/person/source affordances are replaced by
  a pointer to actual Advanced provenance fields; no functioning handler was
  removed with those spans.

The existing native control-room suite passed all 114 tests with no skips.
These are fixture-backed component-logic/parser checks, not a rendered browser
or live integration acceptance pass. No production UI code was changed in this
review. The controller and backend remain separate review scopes, as do the
other module variants and archives. The original template remains intact;
publication, verified preservation on `main` and deletion are separate gates.
