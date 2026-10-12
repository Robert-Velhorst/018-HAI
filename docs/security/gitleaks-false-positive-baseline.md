# Gitleaks False-Positive Baseline

The repository secret scan runs against commits introduced by a pull request.
Gitleaks currently classifies the exact fingerprints in the root
`.gitleaksignore` file as secret-like. They were reviewed and are limited to
public verification material or synthetic test/CI values; no live credentials
are included.

| Fingerprint location | Review |
| --- | --- |
| `openclawmaintenance/pull_client.go:291` | Hexadecimal character allowlist used to validate digest input. |
| `openclawmaintenance/worker.go:69` | Public NPM registry verification key, not a private credential. |
| `safety/redaction_test.go:234` | Deliberately token-shaped input used to test redaction. |
| `workflow/project_dossier_test.go:255,272` | Synthetic AWS-shaped test fixtures. |
| `frontend/scripts/framework-secret-boundary.test.mjs:15` | Invalid private-key-shaped fixture used by a secret-boundary test. |
| `.github/workflows/ci.yml:153,155-159,302` | Disposable static credentials used only by isolated CI test setup. |
| `idp/internal/infra/migrations_test.go:21` | Test-only first-run admin password used by a migration test. |

Each suppression is fingerprint-specific, not a path-wide or rule-wide
exception. Re-review and remove a fingerprint when its source line is removed
or materially changed. Do not add a fingerprint for an unverified or live
secret; rotate any actual credential and remove it from history instead.
