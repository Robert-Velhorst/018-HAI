# Installation administrator and global controls

## Policy decision

Global autonomy approvals belong only to the first-run installation
administrator, not to every account with the `owner` role. This applies to
preparing and deciding approvals as well as executing global changes.

## HTTP boundary

All nine mutating background-control routes now require both administrative
RBAC and the configured installation identity. The identity guard accepts the
IDP's verified owner-role installation scope while retaining the actual token
subject for approval binding and audit attribution. Client-selected headers or
request fields cannot supply this scope.

For compatibility, the existing configured local principal remains accepted
when it has the owner role and no conflicting installation scope. A UUID-based
owner without the signed scope is not sufficient. An empty configuration or a
scope for a different installation fails closed. Read-only background status
and unrelated per-user operations retain their existing authorization rules.

The IDP already issues the installation scope only for the configured first-run
administrator's owner account. This change does not grant that scope to other
accounts, rewrite subjects, mutate deployed accounts, or bypass execution
receipts, exact-effect approvals or emergency-stop checks.

## Verification boundaries

Added signed-JWT middleware cases for the first-run scope, configured legacy
identity, other owner accounts, mismatched scope, operator-role escalation,
missing principal and missing configuration. A route-wiring contract covers all
nine mutations. Full router regressions also assert that another authenticated
owner receives HTTP 403 on every global mutation.

Existing legacy direct-owner confirmation and resume-review service checks
remain intentionally unchanged. Passing this new HTTP guard alone is not proof
that a particular durable approval or resume request will execute: those flows
still enforce their own identity, state and confirmation requirements.

Local execution and CI results must be reported for their actual test target;
this document is not a claim of deployment or live administrator acceptance.
The bounded local identity target (identity middleware, RBAC middleware and
their identity/installation-owner tests) passes ten top-level tests, comprising
28 named test/subtest passes with no failures or skips. This target does not
compile the full router: its nine-route execution regressions still require
the normal backend CI job. No Docker test environment was started.
No original sources, transcripts, databases or worktrees are deleted by this
change.
