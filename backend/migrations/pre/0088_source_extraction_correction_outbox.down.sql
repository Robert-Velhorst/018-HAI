DO $$
BEGIN
    LOCK TABLE public.source_extraction_corrections IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM public.source_extraction_corrections) THEN
        RAISE EXCEPTION 'source extraction correction history exists; preserve it before rolling back migration 0088';
    END IF;
    DROP TABLE public.source_extraction_corrections;
END $$;
