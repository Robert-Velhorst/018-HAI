# Full local connector source reconciliation

Compared canonical main `f7bc8ae30aebfd114cc98ae18848aabf0829f32f` and
published PR #319 head `e02c0814bc7a2bf6b355ec0e010e04de6e3aea14` with the
complete indexed/nonignored source cohort of the retained connector checkout.
Its baseline is `b7e3ad2086c23313a0355101498bee82cda62d4e`, verified as an
ancestor of main. Ignored artifacts and shared Git metadata remain outside
this inventory and must not be inferred disposable.

## Expanded inventory

The earlier 41-file review covered only changed/untracked paths. The expanded
read-only inventory accounts for 3,202 paths with unchanged source status:

| Classification | Paths |
| --- | ---: |
| Current-main blob or ordinary line-ending equivalent | 2,854 |
| Exact/line-ending equivalent only at the selected prepared head | 4 |
| Unchanged baseline files explicitly removed by the ngrok retirement | 10 |
| Remaining full-content comparison cohort | 334 |

Of those 334, 319 match the local baseline after ordinary line-ending handling.
They are older snapshots, not automatically 319 missing features. Fifteen are
locally modified or added and overlap the earlier targeted review. Content
reachability is checked only against selected verified ancestor trees, not
claimed for every blob in all repository history. No file is deletion-cleared.

## Intentional retirement is not a missing feature

Merged PR #208, commit `8e28f14bbd95be6538ed0eaffd175941cf8323c2`, explicitly
retired ngrok launchers, policies, profiles and public cloud instructions to
retain loopback-only operation. That commit is an ancestor of reviewed main.
The earlier audit's suggestion that missing launchers necessarily require a
port was incomplete. Do not restore that intentionally removed subsystem.

This does not by itself dispose of every README/environment difference. The
complete changed files and their other behavior still require successor review.

## Acceptance integration follow-up

The newly run PR #319 browser acceptance exposed a mismatch in the migrated
Quick Capture acceptance test: create requests accept a tag array, but the
persisted memory response uses the existing string-valued `tags` contract.
Its retry additionally reused nearly identical content, which the existing
memory similarity deduplication legitimately merged with the first attempt.

A local follow-up retains the exact persisted identity, summary, content,
draft reload/clear and owned-target assertions, uses the actual string response
contract, and adds distinct synthetic tokens to each attempt's content. It
does not delete existing records, intercept application requests, change
production deduplication, skip the scenario or loosen the persisted-content
assertion. Offline checks are separate from a full browser rerun.

Offline verification checks the test's complete scoped textual difference,
TypeScript syntax, actual response-model declaration, and 1,225 synthetic
fixture pairs. The old fixture's similarity collision is reproduced; the new
pairs have maximum similarity 0.3334, below the unchanged 0.78 threshold. This
is a mirrored fixture check, not execution of the Go backend or a browser run.

The Promptfoo CI job separately reported two high-severity production dependency
advisories. The follow-up updates exactly three lock slots: body-parser 2.3.0
to 2.4.0, its qs dependency 6.16.0 to 6.16.1, and the nested undici 6.29.0 to
6.29.1. The direct manifest, package set, optional packages and unrelated
metadata remain unchanged; all eleven required edges of the three changed
packages resolve within their declared ranges. A fresh production npm audit
reports zero high/critical and one moderate advisory, not zero vulnerabilities.

Only lock metadata was generated using existing Node 26.12.1 and npm 11.6.2,
not the canonical CI npm 12.2.1. Windows initially rejected the Linux-only
libsql dependency, so metadata-only generation used npm's force option with
lifecycle scripts disabled, followed by restricting the output to the three
reviewed slots. No dependencies, providers, images or runtime were installed
or executed. Linux image/build and exact-head CI acceptance remain required.
The security threshold and scanner rules are unchanged.

PR #319 must not merge until the actual remaining CI failures are resolved and
its exact final head passes. Neither a prepared fix nor this report clears any
transcript archive or worktree for removal.
