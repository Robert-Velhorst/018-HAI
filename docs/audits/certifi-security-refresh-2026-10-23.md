# Certifi security refresh

## Advisory and scope

[GHSA-6q48-6c7w-9gw2](https://github.com/advisories/GHSA-6q48-6c7w-9gw2)
identifies compromised intermediate CA keys affecting emSign roots in older
certifi bundles. The advisory's first fixed version is 2026.10.19.

Update certifi from 2026.7.22 to 2026.10.19 in both the hash lock and runtime
requirements for eleven runners: agent-framework, deepeval, deepteam, docling,
evidently, fastmcp, garak, guardrails, lm-eval, miniswe and pydantic-ai.
Only the certifi version and its two distribution hashes change in these
22 manifests; unrelated dependency resolutions are retained.

Docling's image installs its declared Docling version directly rather than
using its CI lock. Add the same fixed certifi version to that actual install
command; changing the lock alone would not constrain this build path.

## Verification and limits

- Confirmed version, Python compatibility, non-yanked distribution status and
  both SHA-256 values from [official PyPI release metadata](https://pypi.org/pypi/certifi/2026.10.19/json).
- Downloaded the official wheel in memory and verified its SHA-256 and package
  version metadata. No persistent download, installed environment or local
  Docker container was created for that check.
- The new policy regression failed for all 22 old manifest entries before
  the update. All five runner CI policy tests pass after the update.
- Existing tests continue requiring matching install-affecting lines in each
  lock/runtime pair. The new regression prevents reintroducing the vulnerable
  version or incorrect hashes and covers Docling's direct install path.
- All 130 local runner-policy, container-smoke-policy and workflow-contract
  tests pass. These are policy/fixture checks, not actual container acceptance.
- Direct service-test attempts pass for Docling (3 tests), garak (4 tests) and
  miniswe (5 tests, including one skip). Eight other suites cannot import their
  locally absent SDKs. No global SDK installation or large local environment
  was created to hide that limitation; the dependency-installed CI suites must
  still run.

Runner dependency installation, built-image acceptance and external provider
behavior remain separate CI/runtime gates. Existing images need rebuilding;
this source change does not patch an already installed Python environment.
Pillow major-version advisories and other dependency findings are not fixed or
dismissed by this change. Original worktrees and archives remain intact.
