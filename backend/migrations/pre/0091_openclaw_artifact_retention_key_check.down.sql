DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.openclaw_artifact_retention_key_check)
       OR EXISTS (SELECT 1 FROM public.openclaw_retained_artifacts) THEN
        RAISE EXCEPTION 'artifact-retention key history or encrypted data exists; refusing to remove the key check';
    END IF;
END
$$;

DROP TABLE public.openclaw_artifact_retention_key_check;
