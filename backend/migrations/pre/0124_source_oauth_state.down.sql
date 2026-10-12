DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.source_o_auth_states
        WHERE consumed_at IS NULL AND expires_at >= now()
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'refusing to discard active OAuth authorization attempts'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TABLE IF EXISTS public.source_o_auth_states;
