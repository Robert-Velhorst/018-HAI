DO $migration$
BEGIN
    RAISE EXCEPTION 'rollback refused: removing host-runtime start intents or acknowledgments could make an ambiguous process start retryable'
        USING ERRCODE = '55000';
END
$migration$;
