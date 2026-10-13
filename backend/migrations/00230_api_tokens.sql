-- +goose Up
-- Personal access tokens, as Stator keeps them. A token belongs to one person in one organization and acts as
-- them there, never more. Only the SHA-256 of the secret is stored, so a copy of this table opens nothing.
CREATE TABLE api_token (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name          text NOT NULL,
    token_hash    bytea NOT NULL UNIQUE,
    -- Empty for a token that may do whatever its owner may; read for one that may only read. Nothing else is
    -- enforced, so nothing else is stored.
    scopes        text[] NOT NULL DEFAULT '{}',
    -- True once a token names its projects. Kept apart from the rows of api_token_project, so a token whose
    -- projects are all deleted reaches nothing rather than everything.
    projects_only boolean NOT NULL DEFAULT false,
    last_used_at  timestamptz,
    expires_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_token_name_length CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT api_token_is_sha256 CHECK (octet_length(token_hash) = 32),
    CONSTRAINT api_token_known_scopes CHECK (scopes <@ ARRAY['read']::text[]),
    -- A foreign key is checked past row level security, so a row that points at a token names its
    -- organization too and cannot cross tenants.
    CONSTRAINT api_token_org_id_key UNIQUE (org_id, id),
    -- Leaving the organization takes the person's tokens there with it.
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

CREATE INDEX api_token_org_user_idx ON api_token (org_id, user_id, created_at DESC);

-- Project ids rather than keys: a renamed project stays in every token that named it, and a deleted one drops
-- out of them.
CREATE TABLE api_token_project (
    org_id     uuid NOT NULL,
    token_id   uuid NOT NULL,
    project_id uuid NOT NULL,
    PRIMARY KEY (token_id, project_id),
    FOREIGN KEY (org_id, token_id) REFERENCES api_token (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

CREATE INDEX api_token_project_project_idx ON api_token_project (org_id, project_id);

-- The projects of the token the transaction acts with, from db.TokenProjectsVar, or NULL for a session and for
-- a token that reaches every project its owner does.
-- +goose StatementBegin
CREATE FUNCTION current_token_projects() RETURNS uuid[]
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.token_projects', true), '')::uuid[]
$$;

-- Whether the transaction's token lets the actor reach a project. The limit is the caller's alone: a rule asked
-- about somebody else, as the access inspector asks, answers for that person.
CREATE FUNCTION perm_token_reaches(actor uuid, project uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT actor IS DISTINCT FROM current_actor_id()
        OR current_token_projects() IS NULL
        OR COALESCE(project = ANY (current_token_projects()), false)
$$;

-- Whether the actor may act on the organization as a whole, which a token limited to projects may not: that is
-- outside every project it names.
CREATE FUNCTION perm_token_whole(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT actor IS DISTINCT FROM current_actor_id() OR current_token_projects() IS NULL
$$;

-- What a person may do in a project, as 00110 says, within the projects the token they act with reaches.
CREATE OR REPLACE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_reaches(actor, perm_project_holds.project) AND EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.org_id = p.org_id AND m.user_id = actor
        WHERE p.id = perm_project_holds.project AND p.org_id = current_org_id()
          AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.org_role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.org_id = p.org_id AND g.project_id = p.id
                AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.org_role, g.role))))))
$$;

-- Administering the organization, which the organization-wide policies ask, is outside a limited token.
CREATE OR REPLACE FUNCTION current_actor_administers() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_whole(current_actor_id()) AND EXISTS (
        SELECT 1 FROM org_member
        WHERE org_id = current_org_id() AND user_id = current_actor_id() AND org_role IN ('owner', 'admin'))
$$;

-- A token is made with its projects, in the transaction that makes it, by its owner, naming only projects they
-- may view; a row added to an older token would widen it.
CREATE FUNCTION api_token_project_addable(token uuid, project uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM api_token t
                   WHERE t.org_id = current_org_id() AND t.id = token AND t.projects_only
                     AND t.user_id = current_actor_id() AND t.created_at = now())
       AND perm_project_holds(project, current_actor_id(), 'view')
$$;

-- What a token is stays what it was made: only its use is noted, and widening one is making another.
CREATE FUNCTION api_token_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.org_id, NEW.user_id, NEW.name, NEW.token_hash, NEW.scopes, NEW.projects_only, NEW.expires_at, NEW.created_at)
       IS DISTINCT FROM (OLD.org_id, OLD.user_id, OLD.name, OLD.token_hash, OLD.scopes, OLD.projects_only, OLD.expires_at, OLD.created_at) THEN
        RAISE EXCEPTION 'api_token_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A token stays what it was made; make a new token instead.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER api_token_fixed BEFORE UPDATE ON api_token
    FOR EACH ROW EXECUTE FUNCTION api_token_fixed();

ALTER TABLE api_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_token FORCE  ROW LEVEL SECURITY;
ALTER TABLE api_token_project ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_token_project FORCE  ROW LEVEL SECURITY;

CREATE POLICY api_token_tenant_isolation ON api_token
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY api_token_project_tenant_isolation ON api_token_project
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- A token is looked up by its hash before any tenant is known.
CREATE POLICY api_token_admin_bypass ON api_token TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY api_token_project_admin_bypass ON api_token_project TO exciter_admin USING (true) WITH CHECK (true);

-- A token is its owner's to see, make and revoke, and the organization's administrators' to see and revoke.
-- Tokens are the organization's business, not a project's, so a limited token does none of it.
CREATE POLICY api_token_seen_by_owner ON api_token AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_token_whole(current_actor_id()) AND (user_id = current_actor_id() OR current_actor_administers()));
CREATE POLICY api_token_made_by_owner ON api_token AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_token_whole(current_actor_id()) AND user_id = current_actor_id() AND last_used_at IS NULL);
CREATE POLICY api_token_revoked_by_owner ON api_token AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_token_whole(current_actor_id()) AND (user_id = current_actor_id() OR current_actor_administers()));
CREATE POLICY api_token_project_seen_with_token ON api_token_project AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_token_whole(current_actor_id())
           AND EXISTS (SELECT 1 FROM api_token t WHERE t.org_id = api_token_project.org_id AND t.id = api_token_project.token_id));
CREATE POLICY api_token_project_added_with_token ON api_token_project AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_token_whole(current_actor_id()) AND api_token_project_addable(token_id, project_id));

-- No UPDATE through the app role: last_used_at moves through the admin role as the token is used, and a row of
-- api_token_project is added with its token and leaves with it or its project.
GRANT SELECT, INSERT, UPDATE, DELETE ON api_token, api_token_project TO exciter_admin;
REVOKE ALL ON api_token, api_token_project FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON api_token TO exciter_app;
GRANT SELECT, INSERT ON api_token_project TO exciter_app;

REVOKE EXECUTE ON FUNCTION api_token_project_addable(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION api_token_project_addable(uuid, uuid) TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS api_token_project;
DROP TABLE IF EXISTS api_token;
DROP FUNCTION IF EXISTS api_token_fixed();
DROP FUNCTION IF EXISTS api_token_project_addable(uuid, uuid);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_actor_administers() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM org_member
        WHERE org_id = current_org_id() AND user_id = current_actor_id() AND org_role IN ('owner', 'admin'))
$$;

CREATE OR REPLACE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.org_id = p.org_id AND m.user_id = actor
        WHERE p.id = perm_project_holds.project AND p.org_id = current_org_id()
          AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.org_role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.org_id = p.org_id AND g.project_id = p.id
                AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.org_role, g.role))))))
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS perm_token_whole(uuid);
DROP FUNCTION IF EXISTS perm_token_reaches(uuid, uuid);
DROP FUNCTION IF EXISTS current_token_projects();
