# GitHub pagination and identity test reconciliation

## Reviewed source scope

Reviewed both retained versions of the GitHub source-owner and incremental
pagination test files: four source versions. Each pair has identical text.
Current tests retain their coverage and additionally pin the exact local HTTP
fixture address instead of broad link-local allowance. Do not restore weaker
network settings. This is not a complete review of the large source service.

Keep tests for exact deployment-token owner binding, HTTPS-only credential
transport, unauthenticated public reads for another owner and rejection of
caller-supplied GitHub evidence. Retain bounded pagination, overlapping commit
windows, adaptive dense-window splitting and overlap deduplication. Keep separate
commit/run cursors, pending-run detail refresh, observed-status timestamps and
no cursor advancement on incomplete imports or failed detail refreshes.

## Exact numeric HTTP decoding

The HTTP decoder converted all JSON numbers to float64 before the existing
exact identifier validator received them. A valid decimal run ID above the
exact float64 integer range was consequently rejected. Conversely, the
fractional value `901.0000000000000001` rounded to integer 901 and could satisfy
an exact pending-run identity check. Both failures reproduced through a local
HTTP fixture before the repair.

Use the standard-library JSON decoder's UseNumber option. The existing
json.Number identifier validation can then inspect the original decimal text.
Numeric display helpers preserve that text too. Parse Actions total_count as a
bounded nonnegative integer in both listing paths, rather than relying on
float64 assertions or truncation; invalid supplied counts fail closed.

Changing from Unmarshal must not accidentally allow a second JSON document or
trailing junk. An explicit EOF check keeps the prior whole-document contract.
Response size limits, safe provider error projection, retry classification,
request cancellation and redirect rejection remain unchanged.

## Verification and preservation

Two HTTP identity regressions failed before the repair. Positive controls cover
ordinary IDs and wrong-ID rejection. Additional checks cover trailing data,
whitespace, nonnegative count bounds, fractions, exponent notation and overflow.
The full source package passed locally with uncached tests on Windows and
Go 1.27.3 after these additional cases were included.
These tests use local fixtures and synthetic records, not real GitHub tokens or
account acceptance.

Private source receipts record the four reviewed test variants. They do not
mark the entire source service reviewed or prove worktree deletion eligibility.
Original worktrees, transcripts and archives remain intact.
