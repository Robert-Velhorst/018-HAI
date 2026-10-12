-- Deliberately irreversible: restoring the old lease-selection semantics can
-- replay expired work or allow already-expired approvals to execute. Refuse
-- rollback without changing schema or data; deploy a separately reviewed
-- forward safety migration instead.
DO $$
BEGIN
    RAISE EXCEPTION 'rollback refused: host runtime expiry and uncertain-outcome protections must not be removed'
        USING ERRCODE = '55000';
END;
$$;
