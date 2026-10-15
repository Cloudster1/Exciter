-- +goose Up
-- Outbound webhooks (#48), as Stator keeps them: an address the installation's administrators named, the events it
-- hears and the projects it listens to, a secret sealed with the server's key, and a log of every attempt.

CREATE TABLE webhook_endpoint (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    name            text NOT NULL,
    url             text NOT NULL,
    secret_sealed   bytea NOT NULL,
    topics          text[] NOT NULL DEFAULT '{}',
    -- The projects it listens to; none is every project its owner may see. A project deleted leaves its id here,
    -- where it matches nothing.
    project_ids     uuid[] NOT NULL DEFAULT '{}',
    enabled         boolean NOT NULL DEFAULT true,
    version         integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    -- Whose rights the payloads are read with: the administrator who saved it last.
    owner_id        uuid REFERENCES app_user (id) ON DELETE SET NULL,
    failures        integer NOT NULL DEFAULT 0,
    failing_since   timestamptz,
    disabled_reason text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT webhook_endpoint_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT webhook_endpoint_url_shape CHECK (url ~ '^https?://[^/?#@\s]+' AND char_length(url) <= 2000),
    CONSTRAINT webhook_endpoint_topics_known CHECK (topics <@ ARRAY['*', 'requirement.created', 'requirement.revised',
        'requirement.changed', 'requirement.trashed', 'requirement.restored', 'requirement.moved', 'link.created', 'link.removed',
        'link.cleared', 'review.requested', 'review.decided', 'review.closed', 'comment.created', 'thread.resolved', 'baseline.taken']),
    CONSTRAINT webhook_endpoint_projects_bounded CHECK (cardinality(project_ids) <= 100),
    CONSTRAINT webhook_endpoint_disabled_reason CHECK (disabled_reason IS NULL OR disabled_reason = 'failing'),
    CONSTRAINT webhook_endpoint_failures CHECK (failures >= 0)
);

CREATE UNIQUE INDEX webhook_endpoint_name_idx ON webhook_endpoint (lower(name));

-- One attempt at one event. A retry is a row of its own, the next attempt; the event is the outbox payload as it
-- was emitted, ids only, and the body is read afresh as the owner may view it on every attempt.
CREATE TABLE webhook_delivery (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    endpoint_id     uuid NOT NULL REFERENCES webhook_endpoint (id) ON DELETE CASCADE,
    event_id        uuid NOT NULL,
    topic           text NOT NULL,
    event           jsonb NOT NULL,
    occurred_at     timestamptz NOT NULL,
    attempt         integer NOT NULL DEFAULT 1,
    manual          boolean NOT NULL DEFAULT false,
    state           text NOT NULL DEFAULT 'pending',
    status          integer,
    error           text NOT NULL DEFAULT '',
    version         integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    next_attempt_at timestamptz,
    attempted_at    timestamptz,
    delivered_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT webhook_delivery_once UNIQUE (endpoint_id, event_id, attempt),
    CONSTRAINT webhook_delivery_attempt CHECK (attempt > 0),
    CONSTRAINT webhook_delivery_state CHECK (state IN ('pending', 'delivered', 'failed', 'withheld', 'cancelled')),
    CONSTRAINT webhook_delivery_due CHECK ((state = 'pending') = (next_attempt_at IS NOT NULL)),
    CONSTRAINT webhook_delivery_event CHECK (jsonb_typeof(event) = 'object')
);

CREATE INDEX webhook_delivery_due_idx ON webhook_delivery (next_attempt_at) WHERE state = 'pending';
CREATE INDEX webhook_delivery_endpoint_idx ON webhook_delivery (endpoint_id, created_at DESC, id DESC);
CREATE INDEX webhook_delivery_created_idx ON webhook_delivery (created_at);

-- Whoever saves an endpoint becomes its owner, and turning one on again starts its count of failures afresh. The
-- worker, acting for nobody, changes its counts without taking it over.
-- +goose StatementBegin
CREATE FUNCTION webhook_endpoint_stamp() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF current_actor_id() IS NOT NULL AND (TG_OP = 'INSERT' OR NEW.owner_id IS NOT DISTINCT FROM OLD.owner_id) THEN
        NEW.owner_id := current_actor_id();
        NEW.updated_at := now();
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.enabled AND NOT OLD.enabled THEN
        NEW.failures := 0;
        NEW.failing_since := NULL;
        NEW.disabled_reason := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER webhook_endpoint_stamp BEFORE INSERT OR UPDATE ON webhook_endpoint
    FOR EACH ROW EXECUTE FUNCTION webhook_endpoint_stamp();

ALTER TABLE webhook_endpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoint FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_endpoint_installation ON webhook_endpoint USING (true) WITH CHECK (true);
CREATE POLICY webhook_delivery_installation ON webhook_delivery USING (true) WITH CHECK (true);

-- Only the installation's administrators, acting with the whole installation, keep endpoints and read their log;
-- a token held to some projects is outside every one of them.
CREATE POLICY webhook_endpoint_administrators ON webhook_endpoint AS RESTRICTIVE FOR ALL TO exciter_app
    USING ((SELECT current_actor_administers())) WITH CHECK ((SELECT current_actor_administers()));
CREATE POLICY webhook_delivery_administrators ON webhook_delivery AS RESTRICTIVE FOR SELECT TO exciter_app
    USING ((SELECT current_actor_administers()));

GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_endpoint, webhook_delivery TO exciter_admin;
REVOKE ALL ON webhook_endpoint, webhook_delivery FROM exciter_app;
-- The sealed secret never leaves the database through the app role, and the log is the worker's to write.
GRANT SELECT (id, name, url, topics, project_ids, enabled, version, owner_id, failures, failing_since, disabled_reason, created_at, updated_at)
    ON webhook_endpoint TO exciter_app;
GRANT INSERT (id, name, url, secret_sealed, topics, project_ids, enabled, version) ON webhook_endpoint TO exciter_app;
GRANT UPDATE (name, url, secret_sealed, topics, project_ids, enabled, version) ON webhook_endpoint TO exciter_app;
GRANT DELETE ON webhook_endpoint TO exciter_app;
GRANT SELECT ON webhook_delivery TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS webhook_delivery;
DROP TABLE IF EXISTS webhook_endpoint;
DROP FUNCTION IF EXISTS webhook_endpoint_stamp();
