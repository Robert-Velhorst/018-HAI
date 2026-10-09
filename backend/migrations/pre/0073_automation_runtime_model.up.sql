ALTER TABLE public.automations
    ADD COLUMN IF NOT EXISTS runtime_model character varying(255) DEFAULT '';
