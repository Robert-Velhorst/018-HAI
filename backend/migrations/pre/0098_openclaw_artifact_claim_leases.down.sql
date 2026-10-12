LOCK TABLE public.openclaw_gateway_artifact_collections IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM public.openclaw_gateway_artifact_collections
         WHERE claim_token IS NOT NULL
            OR claimed_at IS NOT NULL
         LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'cannot roll back OpenClaw artifact claim leases while claim tokens are retained';
    END IF;
END
$$;

DROP INDEX IF EXISTS public.idx_openclaw_artifact_collections_claimed;
DROP INDEX IF EXISTS public.idx_openclaw_artifact_collections_claim_token;
ALTER TABLE public.openclaw_gateway_artifact_collections
    DROP CONSTRAINT IF EXISTS chk_openclaw_artifact_claim_pair,
    DROP COLUMN IF EXISTS claim_token,
    DROP COLUMN IF EXISTS claimed_at;
