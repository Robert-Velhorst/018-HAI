-- Additive authority schema. Never adopt Redis records or backfill live families.
-- Existing JWTs without a family row fail closed and require reauthentication.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '15s';
SELECT pg_advisory_xact_lock(hashtext('hai-idp-session-authority-v1'));

CREATE TABLE IF NOT EXISTS session_families (
    id uuid CONSTRAINT session_families_pkey PRIMARY KEY,
    user_id uuid NOT NULL,
    session_version bigint NOT NULL,
    current_generation bigint NOT NULL,
    current_refresh_uuid uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT fk_session_families_user FOREIGN KEY (user_id)
        REFERENCES users(id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT idx_session_families_current_refresh_uuid UNIQUE (current_refresh_uuid),
    CONSTRAINT ck_session_family_version CHECK (session_version >= 0),
    CONSTRAINT ck_session_family_generation CHECK (current_generation >= 0)
);
CREATE INDEX IF NOT EXISTS idx_session_families_user_id ON session_families (user_id);
CREATE INDEX IF NOT EXISTS idx_session_families_expires_at ON session_families (expires_at);

CREATE TABLE IF NOT EXISTS refresh_rotation_receipts (
    predecessor_uuid uuid CONSTRAINT refresh_rotation_receipts_pkey PRIMARY KEY,
    family_id uuid NOT NULL,
    predecessor_generation bigint NOT NULL,
    replacement_generation bigint NOT NULL,
    replacement_uuid uuid NOT NULL,
    encrypted_pair text NOT NULL,
    consumed_at timestamptz NOT NULL,
    replay_until timestamptz NOT NULL,
    CONSTRAINT fk_refresh_rotation_receipts_family FOREIGN KEY (family_id)
        REFERENCES session_families(id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT idx_refresh_rotation_receipts_replacement_uuid UNIQUE (replacement_uuid),
    CONSTRAINT ck_rotation_predecessor_generation CHECK (predecessor_generation >= 0),
    CONSTRAINT ck_rotation_replacement_generation CHECK (replacement_generation = predecessor_generation + 1),
    CONSTRAINT ck_rotation_pair_size CHECK (length(encrypted_pair) > 0 AND length(encrypted_pair) <= 32768),
    CONSTRAINT ck_rotation_replay_deadline CHECK (replay_until > consumed_at)
);
CREATE INDEX IF NOT EXISTS idx_refresh_rotation_receipts_family_id ON refresh_rotation_receipts (family_id);
CREATE INDEX IF NOT EXISTS idx_refresh_rotation_receipts_replay_until ON refresh_rotation_receipts (replay_until);

-- IF NOT EXISTS is not permission to accept a partial/incompatible schema.
-- Do not silently repair or weaken an existing authority table.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (VALUES
            ('session_families', 'session_families_pkey', 'p'),
            ('session_families', 'fk_session_families_user', 'f'),
            ('session_families', 'idx_session_families_current_refresh_uuid', 'u'),
            ('session_families', 'ck_session_family_version', 'c'),
            ('session_families', 'ck_session_family_generation', 'c'),
            ('refresh_rotation_receipts', 'refresh_rotation_receipts_pkey', 'p'),
            ('refresh_rotation_receipts', 'fk_refresh_rotation_receipts_family', 'f'),
            ('refresh_rotation_receipts', 'idx_refresh_rotation_receipts_replacement_uuid', 'u'),
            ('refresh_rotation_receipts', 'ck_rotation_predecessor_generation', 'c'),
            ('refresh_rotation_receipts', 'ck_rotation_replacement_generation', 'c'),
            ('refresh_rotation_receipts', 'ck_rotation_pair_size', 'c'),
            ('refresh_rotation_receipts', 'ck_rotation_replay_deadline', 'c')
        ) AS required(table_name, constraint_name, constraint_type)
        WHERE NOT EXISTS (
            SELECT 1 FROM pg_constraint AS actual
            WHERE actual.conrelid = to_regclass(required.table_name)
              AND actual.conname = required.constraint_name
              AND actual.contype::text = required.constraint_type
              AND actual.convalidated
        )
    ) THEN
        RAISE EXCEPTION 'durable session authority constraint contract is incomplete';
    END IF;
    IF EXISTS (
        SELECT 1 FROM (VALUES
            ('session_families', 'id', 'uuid', 'NO'),
            ('session_families', 'user_id', 'uuid', 'NO'),
            ('session_families', 'session_version', 'int8', 'NO'),
            ('session_families', 'current_generation', 'int8', 'NO'),
            ('session_families', 'current_refresh_uuid', 'uuid', 'NO'),
            ('session_families', 'expires_at', 'timestamptz', 'NO'),
            ('session_families', 'revoked_at', 'timestamptz', 'YES'),
            ('session_families', 'created_at', 'timestamptz', 'NO'),
            ('session_families', 'updated_at', 'timestamptz', 'NO'),
            ('refresh_rotation_receipts', 'predecessor_uuid', 'uuid', 'NO'),
            ('refresh_rotation_receipts', 'family_id', 'uuid', 'NO'),
            ('refresh_rotation_receipts', 'predecessor_generation', 'int8', 'NO'),
            ('refresh_rotation_receipts', 'replacement_generation', 'int8', 'NO'),
            ('refresh_rotation_receipts', 'replacement_uuid', 'uuid', 'NO'),
            ('refresh_rotation_receipts', 'encrypted_pair', 'text', 'NO'),
            ('refresh_rotation_receipts', 'consumed_at', 'timestamptz', 'NO'),
            ('refresh_rotation_receipts', 'replay_until', 'timestamptz', 'NO')
        ) AS required(table_name, column_name, column_type, nullable)
        WHERE NOT EXISTS (
            SELECT 1 FROM information_schema.columns AS actual
            WHERE actual.table_schema = current_schema()
              AND actual.table_name = required.table_name
              AND actual.column_name = required.column_name
              AND actual.udt_name = required.column_type
              AND actual.is_nullable = required.nullable
        )
    ) THEN
        RAISE EXCEPTION 'durable session authority column contract is incomplete';
    END IF;
END;
$$;
