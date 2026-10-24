# Remaining HAI framework upgrades — runtime candidates and Guardrails port

This proposal starts at #338 source
`e9b045b584e16fff02e97fe3ae23dd48fc70c4b3` and preserves its source head.
It prepares Agent Framework Core 1.23.0 (from 1.22.0), Guardrails AI 0.11.0
(from 0.10.2) and Pydantic AI Slim 2.61.0 (from 2.56.0), with current
Setuptools 84.2.1. Agent Framework OpenAI 1.17.0 is already current stable.

The successful isolated metadata run 40084494575 generated all five requested
profile artifacts. Their archive digests, input/lock/log hashes, SDK identities
and matching tested/source Git trees were verified before importing the new
22-package Agent Framework and 27-package Pydantic AI locks. Their canonical
locks exactly mirror legacy requirements.txt. Docker and CI now use Python
3.15.1 and the same locks; images use digest-pinned current Debian 13 Trixie.
Agent Framework source-build tools are hash-locked. Image builds log observed
interpreter/native/distribution provenance for later reconciliation.

All 22 Agent Framework and 27 Pydantic AI package pins match the current stable
PyPI releases and have no per-version feed advisories or prereleases. This is
metadata evidence, not installed-image or provider acceptance. Fresh complete
source-matching hosted contracts, image builds and health checks are required.

Guardrails' 114-package candidate graph remains metadata-only: its runtime lock
and image are not updated. Its input/runtime-lock mismatch is deliberate draft
preparation and must be resolved before considering a merge. This proposal does
not claim all three framework upgrades complete.

The Pydantic AI graph resolved locally with source builds disabled for target
Python 3.15.1. Agent Framework and Guardrails wheel-only attempts encountered
missing PyYAML wheels. Package source was not executed on the operator host.
The isolated metadata workflow now covers these three profiles as well as the
two existing profiles, retaining generated locks and SDK/source provenance.

## Guardrails interpreter conflict

Current Guardrails 0.11.0 explicitly requires Python >=3.10,<3.14. This was
verified both in the public release metadata and inside its SHA-256-verified
wheel. Current LiteLLM 1.106.3 also excludes Python >=3.15. These restrictions
conflict with the portfolio's latest-Python objective and the existing
Guardrails Docker target 3.14. No application interpreter downgrade is applied
or proposed as completion of that objective.

The metadata resolver uses a digest-pinned Python 3.13.17 SDK **only to inspect
the latest upstream graph**. It is not a runtime selection or compatibility
qualification. A maintained integration/replacement or reviewed compatibility
port is still needed before this runner can meet the full requested end state.
The graph must also be checked for packages without stable releases, parent
constraints and advisories before runtime integration.

The actual candidate audit found nine stable-version gaps, three prerelease-only
OpenTelemetry dependencies, and Filelock 3.32.7 carrying GHSA-v4x8-3wr5-3wq8,
GHSA-9vhv-jxww-xfm5 and GHSA-567q-5qvw-pf92. Its OpenAI is 2.54.0 versus
current 3.37.0. None of these findings is waived or suppressed. That graph is
not imported into the application runtime. Resolving the constraints and
interpreter compatibility remains part of the original full objective.

The parent #338 Trixie image correction is preserved through a history-preserving
proposal-branch merge; no default branch was merged or changed. The earlier
parent e9b045b passed both Garak/Evidently contract/build/health jobs, including
the observed Evidently vendor patch in both CI and the built image. Those
receipts do not qualify the newer Trixie image source.

Existing application/case files, privacy controls and active profiles are
preserved. No new cases or local application suites were authored/run. No
main merge, deployment, billing, host SDK or operator database change occurred.

Primary sources: [Agent Framework Core](https://pypi.org/pypi/agent-framework-core/json),
[Guardrails AI](https://pypi.org/pypi/guardrails-ai/0.11.0/json),
[Pydantic AI Slim](https://pypi.org/pypi/pydantic-ai-slim/json),
and [LiteLLM](https://pypi.org/pypi/litellm/json).
