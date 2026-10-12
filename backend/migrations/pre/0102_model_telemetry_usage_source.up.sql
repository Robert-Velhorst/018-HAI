ALTER TABLE public.model_run_telemetries
    ADD COLUMN usage_source character varying(32) NOT NULL DEFAULT 'estimated';

ALTER TABLE public.model_run_telemetries
    ADD CONSTRAINT chk_model_run_telemetry_usage_source CHECK (
        usage_source IN (
            'provider_reported',
            'provider_reported_partial',
            'estimated',
            'estimated_uncertain',
            'provider_report_invalid'
        )
    );

COMMENT ON COLUMN public.model_run_telemetries.usage_source IS
    'Provenance of input/output token counts: provider-reported, partially reported, estimated, uncertain estimate, or invalid provider usage.';
