ALTER TABLE openclaw_maintenance_targets
    ADD COLUMN check_failures integer NOT NULL DEFAULT 0
    CHECK (check_failures >= 0);
