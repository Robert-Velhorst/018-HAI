CREATE TABLE IF NOT EXISTS public.task_completion_plan_history (
    completion_log_id uuid NOT NULL,
    owner_identity character varying(255) NOT NULL,
    task_plan_id character varying(160) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    request_summary text NOT NULL,
    project_key text DEFAULT '' NOT NULL,
    task_type text NOT NULL,
    success_criteria jsonb DEFAULT '[]'::jsonb NOT NULL,
    completion_status character varying(80) NOT NULL,
    source_payload_digest character(64) NOT NULL,
    CONSTRAINT task_completion_plan_history_pkey PRIMARY KEY (completion_log_id),
    CONSTRAINT fk_task_completion_plan_history_log FOREIGN KEY (completion_log_id)
        REFERENCES public.task_completion_plan_logs(id) ON DELETE RESTRICT,
    CONSTRAINT chk_task_completion_plan_history_owner CHECK (btrim(owner_identity) <> ''),
    CONSTRAINT chk_task_completion_plan_history_plan CHECK (btrim(task_plan_id) <> ''),
    CONSTRAINT chk_task_completion_plan_history_request CHECK (
        btrim(request_summary) <> '' AND char_length(request_summary) <= 16384
    ),
    CONSTRAINT chk_task_completion_plan_history_project CHECK (char_length(project_key) <= 16384),
    CONSTRAINT chk_task_completion_plan_history_type CHECK (
        btrim(task_type) <> '' AND char_length(task_type) <= 16384
    ),
    CONSTRAINT chk_task_completion_plan_history_criteria CHECK (
        jsonb_typeof(success_criteria) = 'array' AND
        octet_length(success_criteria::text) <= 2097152
    ),
    CONSTRAINT chk_task_completion_plan_history_status CHECK (btrim(completion_status) <> ''),
    CONSTRAINT chk_task_completion_plan_history_digest CHECK (
        source_payload_digest ~ '^[0-9a-f]{64}$'
    )
);

CREATE INDEX IF NOT EXISTS idx_task_completion_plan_history_owner_created
    ON public.task_completion_plan_history (owner_identity, created_at DESC, completion_log_id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_completion_plan_history_source
    ON public.task_completion_plan_history (owner_identity, task_plan_id, source_payload_digest);

CREATE OR REPLACE FUNCTION public.hai_enforce_task_completion_history_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.task_completion_plan_logs source
        WHERE source.id = NEW.completion_log_id
          AND source.owner_identity = NEW.owner_identity
          AND source.task_plan_id = NEW.task_plan_id
          AND source.created_at = NEW.created_at
          AND source.completion_status = NEW.completion_status
          AND source.payload_digest = NEW.source_payload_digest
          AND source.payload_json ->> 'id' = NEW.task_plan_id
          AND source.payload_json ->> 'request' = NEW.request_summary
          AND COALESCE(source.payload_json ->> 'projectKey', '') = NEW.project_key
          AND source.payload_json #>> '{intake,taskType}' = NEW.task_type
          AND COALESCE(source.payload_json #> '{intake,successCriteria}', '[]'::jsonb) = NEW.success_criteria
          AND source.payload_json ->> 'completionStatus' = NEW.completion_status
          AND (source.payload_json ->> 'createdAt')::timestamptz = NEW.created_at
    ) THEN
        RAISE EXCEPTION 'task completion history does not match immutable source payload'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.hai_project_task_completion_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO public.task_completion_plan_history (
        completion_log_id, owner_identity, task_plan_id, created_at,
        request_summary, project_key, task_type, success_criteria,
        completion_status, source_payload_digest
    ) VALUES (
        NEW.id, NEW.owner_identity, NEW.task_plan_id, NEW.created_at,
        NEW.payload_json ->> 'request', COALESCE(NEW.payload_json ->> 'projectKey', ''),
        NEW.payload_json #>> '{intake,taskType}',
        COALESCE(NEW.payload_json #> '{intake,successCriteria}', '[]'::jsonb),
        NEW.completion_status, NEW.payload_digest
    );
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_task_completion_plan_history_binding
    ON public.task_completion_plan_history;
CREATE TRIGGER trg_task_completion_plan_history_binding
    BEFORE INSERT ON public.task_completion_plan_history
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_enforce_task_completion_history_binding();

DROP TRIGGER IF EXISTS trg_task_completion_plan_logs_project_history
    ON public.task_completion_plan_logs;
CREATE TRIGGER trg_task_completion_plan_logs_project_history
    AFTER INSERT ON public.task_completion_plan_logs
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_project_task_completion_history();

DROP TRIGGER IF EXISTS trg_task_completion_plan_history_immutable
    ON public.task_completion_plan_history;
CREATE TRIGGER trg_task_completion_plan_history_immutable
    BEFORE UPDATE OR DELETE ON public.task_completion_plan_history
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_task_audit_mutation();

DROP TRIGGER IF EXISTS trg_task_completion_plan_history_no_truncate
    ON public.task_completion_plan_history;
CREATE TRIGGER trg_task_completion_plan_history_no_truncate
    BEFORE TRUNCATE ON public.task_completion_plan_history
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_task_audit_truncate();

INSERT INTO public.task_completion_plan_history (
    completion_log_id, owner_identity, task_plan_id, created_at,
    request_summary, project_key, task_type, success_criteria,
    completion_status, source_payload_digest
)
SELECT id, owner_identity, task_plan_id, created_at,
    payload_json ->> 'request', COALESCE(payload_json ->> 'projectKey', ''),
    payload_json #>> '{intake,taskType}',
    COALESCE(payload_json #> '{intake,successCriteria}', '[]'::jsonb),
    completion_status, payload_digest
FROM public.task_completion_plan_logs
ON CONFLICT (completion_log_id) DO NOTHING;
