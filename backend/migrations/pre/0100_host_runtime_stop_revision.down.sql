DO $migration$
BEGIN
    RAISE EXCEPTION 'rollback refused: removing host-job stop revisions could make approvals from before an emergency-stop cycle executable again'
        USING ERRCODE = '55000';
END
$migration$;
