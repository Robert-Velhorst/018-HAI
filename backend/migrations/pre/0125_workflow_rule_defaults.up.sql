INSERT INTO public.workflow_rules (
    id,
    rule_key,
    name,
    description,
    category,
    enabled,
    created_at,
    updated_at
) VALUES
    ('00000000-0000-4000-8001-000000001001', 'approval.legal_external', 'Legal and government communication is draft-only', 'Legal, government, insurance, housing association, and lawyer messages must be drafted and held for Robert approval before sending.', 'approval', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001002', 'approval.public_posting', 'Public posting requires evidence and approval', 'Public accountability posts, Medium publishing, social posts, and public claims are prepared as drafts only until evidence is linked and Robert approves.', 'approval', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001003', 'approval.financial_limit_25', 'Financial commitments over 25 EUR need approval', 'Payments, paid provider usage, purchases, refunds, quotes, contracts, and commitments over 25 EUR cannot execute automatically.', 'approval', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001004', 'safety.no_permanent_delete', 'Never delete evidence permanently', 'Legal, financial, source, and project files may be archived or marked duplicate, but permanent deletion requires explicit human approval.', 'safety', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001005', 'safety.account_changes', 'Account changes require approval', 'Password, permission, profile, connector, posting, or account-setting changes must be approval-gated.', 'safety', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001006', 'workflow.checklist_required', 'Execution workflows receive checklists', 'Every actionable workflow item gets a concrete checklist before worker execution or completion.', 'workflow', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001007', 'workflow.blocked_has_reason', 'Blocked workflows need owner, reason, and next action', 'Blocked and waiting workflows must record the responsible party, blocker, next action, and follow-up date where possible.', 'workflow', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001008', 'workflow.external_followup', 'External waiting creates follow-up', 'Items waiting for a lawyer, municipality, client, insurer, freelancer, developer, or VA get an open loop with a follow-up date.', 'workflow', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001009', 'workflow.retry_limits', 'Worker retries are durable and capped', 'Failed worker attempts are counted, retried with backoff, and blocked for human review after the retry limit.', 'workflow', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001010', 'verification.before_done', 'Completion requires verification', 'A workflow can only complete through the worker when checklist progress and task verification support completion.', 'verification', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001011', 'verification.claims_need_sources', 'Important factual claims need sources', 'Evidence claims are linked to their source where possible and marked for review when unsupported.', 'verification', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001012', 'developer.github_quality_gate', 'Developer completion requires GitHub evidence', 'Developer claims of completion require branch/commit/build/test/readme evidence before acceptance.', 'developer', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001013', 'content.medium_draft_only', 'Medium articles are draft-only', 'Article workflows may draft, format, and attach a draft link, but publishing remains approval-gated.', 'content', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('00000000-0000-4000-8001-000000001014', 'learning.corrections_feed_memory', 'Corrections become future rules or memory', 'Rejected drafts, project corrections, and tone changes should become reviewable lessons instead of unbounded raw memory.', 'learning', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (rule_key) DO NOTHING;
