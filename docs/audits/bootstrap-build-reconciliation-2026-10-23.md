# Bootstrap and frontend build reconciliation

## Retained files

Reviewed the complete saved model-audit versions of `scripts/generate-secrets.sh`,
`frontend/package.json` and `frontend/angular.json`, then their complete differences
against current code.

The older files do not add missing product behavior. Preserve the current
explicit first-run owner email, normalization/validation and generation of all
seven credentials before emitting an env fragment. Do not restore generation
inside the output here-document, which could leave a partial fragment after an
entropy-process error.

Keep the current aligned frontend package declarations and Node engine policy,
bounded build wrapper, single-worker default, validated resource overrides,
non-watch test command, strict compiler groups and theme/icon/progressive checks.
Keep the non-injected authenticated theme, content-hashed build output, narrower
production icon selection and stricter initial bundle ceiling. Do not restore the
older unbounded direct Angular build, broad production SVG copy or looser budget.
No dependency is installed, upgraded or downgraded by this reconciliation.

## Secret output correction

The generator checked the entropy process's exit status but accepted malformed
successful output. Four synthetic cases returned success with empty, short,
non-hexadecimal or multiline output. Each was observed accepted before the fix.
This is fault-injection evidence, not an observed defect in real OpenSSL or proof
that any existing installation's credentials are invalid.

Each entropy result must now contain exactly 64 hexadecimal characters before it
can be included in the output. Failures return a generic diagnostic without the
candidate value. Generation still finishes completely before stdout is written.

The regression script now runs in the existing backend CI job from the repository
root. Its temporary cleanup verifies the canonical generated directory path and
refuses symlink/foreign targets. Stale README literal checks were updated to the
current migration-source-of-truth and unproven Windows migration-acceptance text;
the historical Trello migration's up/down files are checked directly instead of
requiring the README to repeat an obsolete migration-tail description.

## Verification boundary

- Complete Bash generator/README regression passes after all four negative cases
  failed before the fix. Valid synthetic output, invalid/missing owner email,
  entropy-process failure and zero partial output remain covered.
- A separate actual local OpenSSL execution returned seven distinct 64-hex values
  and the expected normalized synthetic owner email. Values were neither displayed
  nor persisted; no existing credentials were read or replaced. Format/distinctness
  checks do not independently establish cryptographic entropy quality.
- All 19 focused frontend build/theme-manifest/focused-config/cache tests pass.
- All 114 current workflow contracts pass; workflow YAML and both Bash files parse.

No full production build, browser acceptance, service startup, account change,
database or provider acceptance is claimed by these checks. Source review and PR
publication do not authorize deletion of the original recovery files or archives.
