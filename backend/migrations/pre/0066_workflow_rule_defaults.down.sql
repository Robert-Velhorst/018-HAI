DELETE FROM public.workflow_rules
WHERE (id, rule_key) IN (
    ('00000000-0000-4000-8001-000000001001', 'approval.legal_external'),
    ('00000000-0000-4000-8001-000000001002', 'approval.public_posting'),
    ('00000000-0000-4000-8001-000000001003', 'approval.financial_limit_25'),
    ('00000000-0000-4000-8001-000000001004', 'safety.no_permanent_delete'),
    ('00000000-0000-4000-8001-000000001005', 'safety.account_changes'),
    ('00000000-0000-4000-8001-000000001006', 'workflow.checklist_required'),
    ('00000000-0000-4000-8001-000000001007', 'workflow.blocked_has_reason'),
    ('00000000-0000-4000-8001-000000001008', 'workflow.external_followup'),
    ('00000000-0000-4000-8001-000000001009', 'workflow.retry_limits'),
    ('00000000-0000-4000-8001-000000001010', 'verification.before_done'),
    ('00000000-0000-4000-8001-000000001011', 'verification.claims_need_sources'),
    ('00000000-0000-4000-8001-000000001012', 'developer.github_quality_gate'),
    ('00000000-0000-4000-8001-000000001013', 'content.medium_draft_only'),
    ('00000000-0000-4000-8001-000000001014', 'learning.corrections_feed_memory')
);
