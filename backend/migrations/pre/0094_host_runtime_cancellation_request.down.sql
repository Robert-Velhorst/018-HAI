-- This timestamp is the durable evidence that an owner requested Stop while
-- host execution was active. Refuse rollback rather than silently erase it.
DO $$
BEGIN
    RAISE EXCEPTION 'rollback refused: host runtime cancellation request evidence must be preserved'
        USING ERRCODE = '55000';
END;
$$;
