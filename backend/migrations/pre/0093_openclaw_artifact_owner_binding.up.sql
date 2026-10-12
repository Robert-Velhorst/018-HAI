CREATE UNIQUE INDEX IF NOT EXISTS uq_automation_launch_events_artifact_owner_binding
    ON public.automation_launch_events (id, owner_identity, execution_reference);

CREATE UNIQUE INDEX IF NOT EXISTS uq_openclaw_session_receipts_artifact_owner_binding
    ON public.openclaw_gateway_session_receipts (execution_reference, owner_identity);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.openclaw_retained_artifacts AS retained
        LEFT JOIN public.automation_launch_events AS event
            ON event.id = retained.source_event_id
           AND event.owner_identity = retained.owner_identity
           AND event.execution_reference = retained.execution_reference
        LEFT JOIN public.openclaw_gateway_session_receipts AS receipt
            ON receipt.execution_reference = retained.execution_reference
           AND receipt.owner_identity = retained.owner_identity
        WHERE event.id IS NULL OR receipt.execution_reference IS NULL
    ) THEN
        RAISE EXCEPTION 'retained OpenClaw artifacts contain an owner or source-event mismatch; reconcile before adding owner-binding constraints';
    END IF;
END
$$;

ALTER TABLE public.openclaw_retained_artifacts
    ADD CONSTRAINT fk_openclaw_retained_artifacts_owner_event
    FOREIGN KEY (source_event_id, owner_identity, execution_reference)
    REFERENCES public.automation_launch_events (id, owner_identity, execution_reference)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT;

ALTER TABLE public.openclaw_retained_artifacts
    ADD CONSTRAINT fk_openclaw_retained_artifacts_owner_receipt
    FOREIGN KEY (execution_reference, owner_identity)
    REFERENCES public.openclaw_gateway_session_receipts (execution_reference, owner_identity)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT;
