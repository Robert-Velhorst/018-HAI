# Pursuits template reconciliation

## Scope and method

The retained model-maintenance variant of
`frontend/src/app/pages/pursuits/pursuits.component.html` was compared with the
current integration branch. The complete delta (206 added, 12 removed lines) was
read. Angular parsing accounts for event-expression and reactive-form-control
identities through structural templates, not just directly rendered elements.

All retained event-expression identities and reactive form names remain. The
retained/current templates contain 113/120 buttons. Both retain 47 buttons with
implicit type outside native forms; neither has an implicit-type button inside a
native form. This is not a claim that every button is explicitly typed or that
identity-set comparison proves multiplicity and rendered behavior.

## Preserved behavior

- Existing pursuit intake, lifecycle, review, context, links, resource accounting,
  planning, portfolio allocation, approval, execution, settlement, evidence, and
  source-navigation actions remain represented in the template.
- Loading flags distinguish summary refresh, context saving, and link saving.
  Mutating or loading pursuit state disables conflicting operations. Association
  detachment is described separately from deleting an underlying source.
- Link confidence must be greater than zero and no more than one, consistent
  with the existing controller/server contract.
- Unconfirmed runtime cancellation exposes a review-only recovery action. The
  view does not equate a cancellation request with a confirmed stopped runtime.

## Project dossier addition

Basic shows bounded, read-only workflow/memory counts with the selected project,
generation time, explicit missing-key/loading/error states, and truncation notice.
Advanced exposes returned checklist items, open loops, evidence, source links,
decisions, memories, and individual truncation causes. These records do not grant
execution authority.

Current controller context clears the previous dossier, cancels its observation,
uses a request generation to ignore superseded replies, and rejects a returned
project key different from the requested key. Source links use the existing
scheme/credential/control-character checks and evidence resolution; the template
does not directly turn arbitrary source values into browser navigation.

## Limits

The existing Pursuits Angular suite passed 69 tests in Chrome Headless on this
integration state. Its dossier cases compile/render Basic and Advanced views,
exercise error/retry, and reject a different returned project key. No production
code was changed for this template review.

The retained Pursuits controller and test-file variants still need their separate
full reconciliation. Reading their relevant dossier/navigation contexts does not
complete that work. This template review does not prove live API ownership,
runtime cancellation, provider availability, production viewport quality, or
every dependent component. No source, worktree, or transcript archive becomes
deletion-eligible merely because of this review.
