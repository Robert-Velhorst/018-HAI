# Source-history continuation regression

Prepared baseline: `0589762d5e8e49f018cf428d59a95e0f095bd897`.
Current remote main remains a separate preservation target; this fix is not
claimed to be published or main-integrated.

## Defect and scoped repair

The shared connected-source history validator accepted an empty items array
with a positive remaining total and `hasMore: true`. Its arithmetic continuation
check agrees with that payload, despite there being no record progress to use
for the next page. This affects paged audit logs, sync-job history and the
prepared extraction-history validator. Actual browser repetition was not run.

The repair rejects an empty page that claims continuation. It does not reject
a genuinely empty dataset or an empty terminal page. Existing record validation,
offset/limit matching, count checks, correction concurrency and destructive
authorization headers are unchanged. Three regression cases are added to the
existing Angular service spec, one per history operation.

## Reproduced red/green evidence

An isolated runner executes the trusted prepared service with installed real
RxJS and HTTP/parameter doubles. Historical archive programs are not executed.
For each of audit, sync-job and extraction history, it checks an impossible empty
continuation, an empty dataset, an empty terminal page and a valid record page.

Before the repair, all three empty-continuation rejection assertions fail while
the nine valid cases pass. After the repair, all twelve cases pass. No network,
database, provider, source deletion or actual sync operation occurs.

A focused strict no-emit TypeScript check of the modified service and its full
service spec reports zero diagnostics. The two imported product source files
match the prepared baseline by normalized Git blob identity. Installed third-
party declaration checking is skipped; Angular template checks, Angular TestBed
execution, the complete frontend suite and browser acceptance are not performed.

The initial standalone compiler host could not locate existing Jasmine types
because its configuration/current-directory context was wrong. Correcting the
host to use the frontend config location and frontend working directory resolved
those harness diagnostics without installing packages, adding fake test globals
or changing product TypeScript configuration.

## Integration and cleanup boundary

This is an additional prepared code repair discovered during historical-client
reconciliation, not evidence that the entire historical source cohort is
integrated. Exact-head CI, publication, main preservation and remaining archive
requirements are still open. No archive becomes deletion-eligible from these
focused checks.
