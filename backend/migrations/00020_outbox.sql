-- +goose Up
-- The transactional outbox, as in Armature and Stator: an event is written in the same transaction as the
-- change that caused it, so the change and what follows from it commit together or not at all. The worker
-- claims unprocessed rows with FOR UPDATE SKIP LOCKED and marks them done.
CREATE TABLE outbox_event (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    topic        text NOT NULL,
    payload      jsonb NOT NULL,
    -- The W3C traceparent of the request that emitted it, so the worker's spans join that trace.
    trace_parent text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    attempts     integer NOT NULL DEFAULT 0,
    last_error   text,
    -- A failed event waits before it is tried again, longer each time.
    available_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_event_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT outbox_event_topic_shape CHECK (topic ~ '^[a-z]+(_[a-z]+)*\.[a-z]+(_[a-z]+)*$')
);

-- The worker only ever scans the unprocessed tail, so the index stays small however much history piles up.
CREATE INDEX outbox_event_pending_idx ON outbox_event (available_at, created_at) WHERE processed_at IS NULL;

ALTER TABLE outbox_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_event FORCE  ROW LEVEL SECURITY;

CREATE POLICY outbox_event_tenant_isolation ON outbox_event
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY outbox_event_admin_bypass ON outbox_event TO exciter_admin USING (true) WITH CHECK (true);

-- A request only ever adds events, and only in the name of the person it acts for, so nobody can make the
-- worker tell others something they did not do.
CREATE POLICY outbox_event_own_acts ON outbox_event AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (payload ->> 'actorId' = current_actor_id()::text);

GRANT SELECT, INSERT, UPDATE, DELETE ON outbox_event TO exciter_admin;
REVOKE ALL ON outbox_event FROM exciter_app;
GRANT INSERT ON outbox_event TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS outbox_event;
