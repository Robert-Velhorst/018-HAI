DO $$
BEGIN
    RAISE EXCEPTION 'OpenClaw artifact alignment requires manual reconciliation before rollback; preserve canonical and legacy data';
END $$;
