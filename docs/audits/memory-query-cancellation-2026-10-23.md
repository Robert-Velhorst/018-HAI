# Memory query cancellation boundary

## Finding

The owner-scoped memory query service accepted a request context but did not
check cancellation itself. Its legacy repository path has no context parameter;
an already-cancelled request could therefore still read and return a successful
page. A paginated repository that ignored cancellation could also return a
successful page after cancellation. A nil context was passed through unchanged.

## Change

- Keep owner validation before repository access.
- Normalize nil contexts to a background context.
- Reject cancelled contexts before either repository path starts.
- Check cancellation again before returning a successful page, including after
  in-memory filtering and pagination in the legacy path.
- Preserve repository failures rather than returning their partial results.

## Evidence

Regression tests failed before the production change for pre-read and
during-read cancellation on both repository paths. The nil-context paginated
query also failed before the change. After the repair, the complete memory
package passed locally with Go 1.27.3 on Windows:

```text
go test -p 1 -timeout 180s ./internal/memory -count=1
ok automation-hub-backend/internal/memory
```

## Limits and preservation

These checks enforce the service entry/result boundary; they do not interrupt
an in-flight context-free legacy database call. They do not establish live
provider acceptance or replace PostgreSQL integration testing. This repair is
not evidence that any retained worktree or transcript is fully integrated or
safe to delete. Original source copies remain intact.
