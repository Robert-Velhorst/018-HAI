DO $$ BEGIN
    RAISE EXCEPTION 'Retained artifact rollback requires an explicit export and retention review; automatic deletion is refused';
END $$;
