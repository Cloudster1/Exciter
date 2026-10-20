-- +goose Up
-- The suite demo (#270): the requirements of Armature, Exciter and Stator from their own issues, which an
-- administrator adds once. One row says how far it got and what it made; the worker builds it as the person who
-- asked, and links it to Armature and Stator again whenever somebody asks for that.
CREATE TABLE suite_demo (
    state        text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'failed')),
    -- build makes the projects and links them; links only adds the links that are missing.
    kind         text NOT NULL DEFAULT 'build' CHECK (kind IN ('build', 'links')),
    -- The day the snapshot was taken, and the Exciter keys of the projects it made, in the order of the tools.
    taken        text NOT NULL,
    project_keys text[] NOT NULL,
    requested_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    -- The worker takes the run a short step an event, so other work goes on between: the next step and how many.
    step         integer NOT NULL DEFAULT 0 CHECK (step >= 0),
    steps        integer NOT NULL DEFAULT 0 CHECK (steps >= step),
    requirements integer NOT NULL DEFAULT 0 CHECK (requirements >= 0),
    links        integer NOT NULL DEFAULT 0 CHECK (links >= 0),
    -- What could not be linked: a partner not connected, an issue or a page it does not have yet.
    skipped      integer NOT NULL DEFAULT 0 CHECK (skipped >= 0),
    message      text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    started_at   timestamptz,
    finished_at  timestamptz
);
CREATE UNIQUE INDEX suite_demo_one_row_idx ON suite_demo ((true));

-- +goose StatementBegin
-- The demo moves forward only, a step at a time, and is asked for again once it ended; what it was made from never
-- changes.
CREATE FUNCTION suite_demo_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.taken, NEW.project_keys, NEW.created_at) IS DISTINCT FROM (OLD.taken, OLD.project_keys, OLD.created_at) THEN
        RAISE EXCEPTION 'suite_demo_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The suite demo keeps the snapshot and the projects it was made with.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'queued' AND NEW.state = 'running')
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed'))
        OR (OLD.state IN ('done', 'failed') AND NEW.state = 'queued')) THEN
        RAISE EXCEPTION 'suite_demo_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'The suite demo moves from queued to running, and then ends; an ended one may be queued again.';
    END IF;
    IF NEW.step < OLD.step AND NEW.state <> 'queued' THEN
        RAISE EXCEPTION 'suite_demo_step' USING ERRCODE = 'check_violation',
            DETAIL = 'The suite demo goes back to its first step only when it is queued again.';
    END IF;
    IF NEW.kind IS DISTINCT FROM OLD.kind AND NEW.state <> 'queued' THEN
        RAISE EXCEPTION 'suite_demo_kind' USING ERRCODE = 'check_violation',
            DETAIL = 'What the suite demo does next is chosen when it is queued.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER suite_demo_guard BEFORE UPDATE ON suite_demo
    FOR EACH ROW EXECUTE FUNCTION suite_demo_guard();

-- Which issue, or which of its criteria, a requirement of the demo was made from, so linking again finds it.
-- Issue 0 is the requirement that says what the tool is; criterion 0 is the issue's own requirement.
CREATE TABLE suite_demo_requirement (
    requirement_id uuid PRIMARY KEY REFERENCES requirement(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    tool           text NOT NULL CHECK (tool ~ '^[A-Z]{3}$'),
    issue          integer NOT NULL CHECK (issue >= 0),
    criterion      integer NOT NULL CHECK (criterion >= 0),
    CONSTRAINT suite_demo_requirement_once UNIQUE (project_id, issue, criterion)
);

ALTER TABLE suite_demo ENABLE ROW LEVEL SECURITY;
ALTER TABLE suite_demo FORCE  ROW LEVEL SECURITY;
ALTER TABLE suite_demo_requirement ENABLE ROW LEVEL SECURITY;
ALTER TABLE suite_demo_requirement FORCE  ROW LEVEL SECURITY;

CREATE POLICY suite_demo_installation ON suite_demo USING (true) WITH CHECK (true);
CREATE POLICY suite_demo_requirement_installation ON suite_demo_requirement USING (true) WITH CHECK (true);

-- The demo is an administrator's to add and to read, and the worker acts as the administrator who asked.
CREATE POLICY suite_demo_admins ON suite_demo AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
CREATE POLICY suite_demo_requirement_admins ON suite_demo_requirement AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());

GRANT SELECT, INSERT, UPDATE, DELETE ON suite_demo, suite_demo_requirement TO exciter_admin;
REVOKE ALL ON suite_demo, suite_demo_requirement FROM exciter_app;
GRANT SELECT, INSERT, UPDATE ON suite_demo TO exciter_app;
GRANT SELECT, INSERT ON suite_demo_requirement TO exciter_app;

-- +goose Down
DROP TABLE suite_demo_requirement;
DROP TABLE suite_demo;
DROP FUNCTION suite_demo_guard();
