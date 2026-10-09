DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM openclaw_gateway_session_receipts LIMIT 1) THEN
        RAISE EXCEPTION
            'cannot roll back OpenClaw Gateway receipt ledger while records exist';
    END IF;
END
$$;

DROP TABLE IF EXISTS openclaw_gateway_session_receipts;
