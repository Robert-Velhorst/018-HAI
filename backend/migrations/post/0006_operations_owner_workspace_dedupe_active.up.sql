-- One active source revision may create at most one operation for an owner and
-- workspace. Archived and dismissed records remain as historical evidence and
-- do not block a later re-intake of the same source item.
CREATE UNIQUE INDEX IF NOT EXISTS uq_operations_owner_workspace_dedupe_active
    ON public.operations (owner_user_id, workspace_id, dedupe_key)
    WHERE status NOT IN ('archived', 'dismissed');
