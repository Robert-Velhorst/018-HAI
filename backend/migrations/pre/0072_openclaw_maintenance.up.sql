CREATE TABLE IF NOT EXISTS openclaw_maintenance_targets (
 id text PRIMARY KEY CHECK (id IN ('companion','gateway_core')),
 policy text NOT NULL DEFAULT 'observe' CHECK (policy IN ('observe','auto_install_verified')),
 installed text NOT NULL DEFAULT '', available text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'unknown', reason text NOT NULL DEFAULT '',
 checked_at timestamptz, next_check timestamptz NOT NULL DEFAULT now(), verified_at timestamptz,
 receipt_id text NOT NULL DEFAULT '', updated_by text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS openclaw_maintenance_jobs (
 id text PRIMARY KEY, target text NOT NULL REFERENCES openclaw_maintenance_targets(id),
 kind text NOT NULL CHECK (kind IN ('status','check','apply','policy','review')),
 version text NOT NULL DEFAULT '', status text NOT NULL,
 evidence text NOT NULL DEFAULT '', worker text NOT NULL DEFAULT '', lease_digest text NOT NULL DEFAULT '',
 lease_until timestamptz, started_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz, result text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_openclaw_maintenance_active_target
 ON openclaw_maintenance_jobs(target) WHERE status IN ('pending','leased');
CREATE INDEX IF NOT EXISTS idx_openclaw_maintenance_history ON openclaw_maintenance_jobs(target,created_at DESC);
