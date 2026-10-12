DO $$ BEGIN
    RAISE EXCEPTION 'OpenClaw receipt alignment requires a reviewed data-preserving rollback; automatic rollback is disabled';
END $$;
