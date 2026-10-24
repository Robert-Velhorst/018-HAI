# Remaining HAI framework upgrades — preparation

This proposal starts at #338 source
`e9b045b584e16fff02e97fe3ae23dd48fc70c4b3` and preserves its source head.
It prepares Agent Framework Core 1.23.0 (from 1.22.0), Guardrails AI 0.11.0
(from 0.10.2) and Pydantic AI Slim 2.61.0 (from 2.56.0), with current
Setuptools 84.2.1. Agent Framework OpenAI 1.17.0 is already current stable.

Only input declarations and the public-only metadata resolver are changed at
this preparation stage. Runtime locks, images, application SDK selections and
application behavior are not yet updated to these graphs. Ordinary application
CI against old locks must not be interpreted as acceptance of the new inputs.
Keep this a draft until graph integration and runtime qualification are done.

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

Existing application/case files, privacy controls and active profiles are
preserved. No new cases or local application suites were authored/run. No
main merge, deployment, billing, host SDK or operator database change occurred.

Primary sources: [Agent Framework Core](https://pypi.org/pypi/agent-framework-core/json),
[Guardrails AI](https://pypi.org/pypi/guardrails-ai/0.11.0/json),
[Pydantic AI Slim](https://pypi.org/pypi/pydantic-ai-slim/json),
and [LiteLLM](https://pypi.org/pypi/litellm/json).
