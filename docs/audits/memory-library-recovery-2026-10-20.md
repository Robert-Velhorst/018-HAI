# Memory library retained-UI recovery

## Reconciliation

Retained Memory variants included filter clearing and page-range feedback.
Current main already has server-side filtering, paging, applied-versus-draft
query separation, lazy Advanced loading, ownership enforcement in the backend,
and cancellation/error recovery. Replacing those with the older query pipeline
would regress existing safeguards, so only the missing UI behaviors are ported.

## Behavior

- Clear search, kind, tag, sort and order in one explicit action.
- Keep the applied project, applied page size and archive inclusion unchanged;
  an unsubmitted project draft cannot silently widen the scope of this action.
- Reset to the first page using the existing applied-filter pipeline, including
  its stale-selection and retrieval invalidation.
- Ignore direct reset calls while loading or in Basic mode. Disable the rendered
  control while loading; provide a tooltip describing the retained scope.
- Show the server page's actual record range and total, including partial final
  pages and a successfully loaded empty result. Hide the range while loading,
  after a failed load, or before results exist. Do not infer inventory totals
  from the number of records currently rendered.

## Verification scope

Regression tests cover retained scope, draft-versus-applied values, loading and
Basic guards, partial and empty ranges, and the rendered button through the real
Angular HTTP client with synthetic fixtures. They are not evidence of live
account data, deployed browser layout, or complete archive reconciliation.

The canonical production build and full npm test passed, including preflight
guards, 180 Node tests and 1,424 Angular tests. Local execution used Node 26.12.0,
npm 11.6.2 and the existing dependency tree; fresh locked installation remains
a hosted CI gate. Existing style-budget warnings remain; no budget was raised.
The retained source was rehashed and still matches its inventoried variant.

No backend contract, ownership policy, approval gate, or stored memory record is
changed. Original archives and source worktrees are retained; recovering these
methods does not prove that every other retained difference is integrated.
