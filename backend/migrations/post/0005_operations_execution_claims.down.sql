DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operation_execution_claims) THEN
        RAISE EXCEPTION 'cannot remove Operation claim generations while durable fencing history exists';
    END IF;
END $$;

DROP TABLE public.operation_execution_claims;
