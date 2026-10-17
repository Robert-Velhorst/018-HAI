-- Stop all connector processes first. This removes progress only, never ledger
-- records. Reapplying UP requires a safe full replay. Do not use for source resets.
DROP TABLE public.account_feed_reference_checkpoints;
