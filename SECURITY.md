# Security Policy

## System and scope

018-HAI is a local-first Human Autonomous Intelligence shell for authorized source material, durable memory, workflows, approvals, and controlled execution. This policy covers this repository's Go services and APIs, Angular dashboard, PostgreSQL persistence, Docker Compose runtime, and source, connector, workflow, approval, and execution integrations. It covers HAI's security boundaries; it does not certify the operator's host, deployment configuration, or third-party providers.

The product's documented operating stance is local-first and approval-gated. Planning, execution, verification, and approval are separate stages. Real external effects require an explicit human approval and, where required, verified credentials. See `docs/compliance-boundaries.md`, `docs/data-model.md`, and `docs/agent-tool-catalog.md`.

## Threat model and trust boundaries

Treat HTTP requests, imported documents and messages, connected-source data, external connector responses, model/provider output, catalog metadata, tool results, and persisted memories as potentially attacker-controlled. These are data inputs; they do not grant identity, permission, approval, or execution authority.

Security boundaries include authenticated user and project ownership, API authorization, connector consent and scopes, source provenance, workflow/runtime selection, permission and execution admission, human approval, final-effect authorization, persistence and export, and the operator-controlled runtime. External providers and local deployments have independent security and configuration that this repository cannot establish by itself.

## Security invariants

- Every user-owned record and operation must remain scoped to its authenticated owner and project; a caller must not read or mutate another project's data.
- Authentication and authorization must be checked before protected reads, writes, exports, connector calls, or execution.
- Untrusted content must never widen source scope, choose or authorize a tool, change a permission, satisfy or replay an approval, or override policy. Planning or a recommendation is not authorization to execute.
- Before an external effect, the runtime must enforce the current policy and bounded allowlist, obtain the required explicit approval, verify any required credential and final-effect authorization, and retain truthful provenance. Emergency-stop controls must be able to halt bounded automation.
- Read-only integrations remain read-only unless a separately reviewed and approved capability explicitly grants a bounded write scope. Paid-provider use requires server-side approval; the documented default paid budget is zero.
- Secrets and unnecessary personal or source content must not be exposed in public issues, logs, errors, exports, or security-policy material. Preserve source provenance and redact sensitive content from inspection surfaces.

## Reportable findings and severity

Report reachable defects that bypass authentication, owner/project isolation, connector consent, permission checks, approval or final-effect authorization; expose secrets or sensitive source/memory data; allow hostile imported or provider content to cause unauthorized execution or data disclosure; or create materially unsafe file, network, command, or resource access.

Severity should reflect realistic reachability, required attacker privileges, affected users and data, and impact. Unauthorized external execution, broad cross-project disclosure, or credential compromise is typically critical. A scoped authorization bypass or sensitive-data disclosure is typically high. Lower-impact, constrained defects may be medium or low when the impact and prerequisites support that rating. Do not suppress a finding solely because a control is intended, documented, or covered by a test.

## Out of scope and accepted risk

The repository does not authorize blanket exclusions or accept risks by default. A flaw solely inside a third-party provider is outside this code review boundary; flaws in HAI's integration, credential handling, source validation, or policy enforcement remain in scope. Any accepted risk must be explicitly confirmed by the owner and tracked with its scope, rationale, compensating controls, and review date.

## Known limitations

A code review cannot prove the security of a particular operator host, live deployment, configured account, third-party service, or paid-provider setup. Connector adapters and runtime capabilities must be assessed with their actual configuration and privileges. A passing test or scan is evidence for the code it covers, not proof of end-to-end deployment safety.

## Reporting a vulnerability

Do not publish vulnerability details, secrets, proof-of-concept code, or exploit instructions in a public issue, discussion, or pull request. If GitHub shows **Report a vulnerability** in this repository's Security tab, use that private reporting form.

If private reporting is unavailable, contact the repository maintainers through a private contact method listed on the repository owner's GitHub profile. If you cannot find one, open an issue asking for a private disclosure channel and include no vulnerability details until a private channel is available.

## Response and disclosure

Reports are reviewed on a best-effort basis; no response or fix-time guarantee is provided. Coordinate public disclosure with the maintainers so users have a reasonable opportunity to receive a fix. Security fixes target the current default branch and latest published release; older versions are not guaranteed to receive fixes.

## Supported versions

Security fixes are assessed against the current default branch and latest published release. Older versions are not guaranteed to receive fixes unless release notes explicitly identify them as maintained.
