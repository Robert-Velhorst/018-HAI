DO $$
BEGIN
    RAISE EXCEPTION 'rollback refused: host-runtime cancellation safety depends on the durable execution confirmation fence; preserve the column and its evidence'
        USING ERRCODE = '55000';
END
$$;
