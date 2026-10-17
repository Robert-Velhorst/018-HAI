-- Stop all connector processes and remove connector configuration first.
-- Removing this table loses disabled state. Retain a backup before rollback.
DROP TABLE public.account_feed_reference_controls;
