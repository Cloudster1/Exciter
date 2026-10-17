-- +goose Up
-- Exciter as the authorization server of its MCP endpoint (#188), as docs/api-contract-m7.md agrees: a client is
-- known, a person consents, and the client gets tokens for MCP alone. Requests, codes and tokens are used before
-- anybody is signed in, so the admin role alone reads them, as it reads sessions; a grant and its projects are
-- their owner's and the administrators', under row level security like a personal token.

ALTER TABLE installation ADD COLUMN oauth_connections boolean NOT NULL DEFAULT true;

-- A client is its client_id: an id Exciter gave it, or the address of its metadata document.
CREATE TABLE oauth_client (
    id              text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 2048),
    kind            text NOT NULL CHECK (kind IN ('registered', 'metadata')),
    name            text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    uri             text NOT NULL DEFAULT '',
    redirect_uris   text[] NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10),
    auth_method     text NOT NULL DEFAULT 'none' CHECK (auth_method IN ('none', 'client_secret_basic', 'client_secret_post')),
    secret_hash     bytea CHECK (octet_length(secret_hash) = 32),
    grant_types     text[] NOT NULL CHECK (grant_types <@ ARRAY['authorization_code', 'refresh_token']::text[]),
    -- The address a registration came from, which its rate is counted by; null for a metadata document.
    registered_from text,
    -- Until when a metadata document may be used without fetching it again; null for a registration.
    fresh_until     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- When its last grant ended, which keeps a client that was connected for a while after; null until then.
    idle_since      timestamptz,
    CONSTRAINT oauth_client_secret_when_asked CHECK ((auth_method = 'none') = (secret_hash IS NULL)),
    CONSTRAINT oauth_client_documents_keep_no_secret CHECK (kind = 'registered' OR auth_method = 'none'),
    CONSTRAINT oauth_client_documents_are_fetched CHECK ((kind = 'metadata') = (fresh_until IS NOT NULL))
);

CREATE INDEX oauth_client_registered_idx ON oauth_client (registered_from, created_at) WHERE kind = 'registered';

-- An authorization waiting for its person, under an id as random as a session's, for RequestTTL.
CREATE TABLE oauth_request (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id      text NOT NULL REFERENCES oauth_client(id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    state          text NOT NULL,
    code_challenge text NOT NULL,
    read_only      boolean NOT NULL,
    resource       text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL
);

-- What a person let one client do: a client holds one grant for each person.
CREATE TABLE oauth_grant (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    client_id     text NOT NULL REFERENCES oauth_client(id) ON DELETE CASCADE,
    redirect_uri  text NOT NULL,
    read_only     boolean NOT NULL,
    -- As a token's: kept apart from the rows of oauth_grant_project, so a grant whose projects are all deleted
    -- reaches nothing rather than everything.
    projects_only boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz,
    CONSTRAINT oauth_grant_one_per_person UNIQUE (client_id, user_id)
);

CREATE INDEX oauth_grant_user_idx ON oauth_grant (user_id, created_at DESC);

CREATE TABLE oauth_grant_project (
    grant_id   uuid NOT NULL REFERENCES oauth_grant(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    PRIMARY KEY (grant_id, project_id)
);

CREATE INDEX oauth_grant_project_project_idx ON oauth_grant_project (project_id);

-- A code is used once, for CodeTTL; a used one is kept until then, so its second use ends the grant it made.
CREATE TABLE oauth_code (
    code_hash      bytea PRIMARY KEY CHECK (octet_length(code_hash) = 32),
    grant_id       uuid NOT NULL REFERENCES oauth_grant(id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    resource       text NOT NULL,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz
);

-- An access token for AccessTTL, or a refresh token, which a newer one replaces; one used once replaced is a
-- stolen one, and ends the grant.
CREATE TABLE oauth_token (
    token_hash  bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    grant_id    uuid NOT NULL REFERENCES oauth_grant(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('access', 'refresh')),
    read_only   boolean NOT NULL,
    expires_at  timestamptz NOT NULL,
    replaced_at timestamptz CHECK (replaced_at IS NULL OR kind = 'refresh'),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_token_grant_idx ON oauth_token (grant_id);

-- +goose StatementBegin
-- A grant is made with its projects, in the transaction that makes it, by its owner, naming only projects they may
-- view; a row added to an older grant would widen it.
CREATE FUNCTION oauth_grant_project_addable(grant_id uuid, project uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM oauth_grant g
                   WHERE g.id = grant_id AND g.projects_only
                     AND g.user_id = current_actor_id() AND g.created_at = now())
       AND perm_project_holds(project, current_actor_id(), 'view')
$$;

-- What a grant is stays what it was made: only its use is noted, and widening one is consenting again.
CREATE FUNCTION oauth_grant_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.user_id, NEW.client_id, NEW.redirect_uri, NEW.read_only, NEW.projects_only, NEW.created_at)
       IS DISTINCT FROM (OLD.user_id, OLD.client_id, OLD.redirect_uri, OLD.read_only, OLD.projects_only, OLD.created_at) THEN
        RAISE EXCEPTION 'oauth_grant_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A grant stays what it was made; connect the client again instead.';
    END IF;
    RETURN NEW;
END;
$$;

-- A client whose last grant ended is idle from then, which the worker counts its removal from.
CREATE FUNCTION oauth_grant_ended() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    UPDATE oauth_client SET idle_since = now()
    WHERE id = OLD.client_id AND NOT EXISTS (SELECT 1 FROM oauth_grant g WHERE g.client_id = OLD.client_id);
    RETURN NULL;
END;
$$;

-- Switching somebody off, or taking their role, ends their grants, as it ends their tokens (#86).
CREATE FUNCTION app_user_ends_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM oauth_grant WHERE user_id = NEW.id;
    RETURN NULL;
END;
$$;

-- A request decided is taken whole, by whoever consents, though the app role cannot read requests: the id is what
-- the consent page was handed, and an expired request is gone.
CREATE FUNCTION oauth_request_take(request uuid) RETURNS SETOF oauth_request
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    DELETE FROM oauth_request WHERE id = request AND expires_at > now() RETURNING *
$$;
-- +goose StatementEnd

CREATE TRIGGER oauth_grant_fixed BEFORE UPDATE ON oauth_grant
    FOR EACH ROW EXECUTE FUNCTION oauth_grant_fixed();
CREATE TRIGGER oauth_grant_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON oauth_grant
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('user_id');
CREATE TRIGGER oauth_grant_ended AFTER DELETE ON oauth_grant
    FOR EACH ROW EXECUTE FUNCTION oauth_grant_ended();
CREATE TRIGGER app_user_ends_grants AFTER UPDATE OF is_active, role ON app_user
    FOR EACH ROW WHEN ((OLD.is_active AND NOT NEW.is_active) OR (OLD.role IS NOT NULL AND NEW.role IS NULL))
    EXECUTE FUNCTION app_user_ends_grants();

ALTER TABLE oauth_client ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_client FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_request FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_grant FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_grant_project ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_grant_project FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_code ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_code FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_token FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY oauth_client_installation ON oauth_client USING (true) WITH CHECK (true);
CREATE POLICY oauth_request_installation ON oauth_request USING (true) WITH CHECK (true);
CREATE POLICY oauth_grant_installation ON oauth_grant USING (true) WITH CHECK (true);
CREATE POLICY oauth_grant_project_installation ON oauth_grant_project USING (true) WITH CHECK (true);
CREATE POLICY oauth_code_installation ON oauth_code USING (true) WITH CHECK (true);
CREATE POLICY oauth_token_installation ON oauth_token USING (true) WITH CHECK (true);

-- A client is shown to those signed in, beside the grants that name it.
CREATE POLICY oauth_client_seen_by_members ON oauth_client AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role IS NOT NULL));

-- A grant is its owner's to see, make and end, and the administrators' to see and end. Grants are the
-- installation's business, not a project's, so a limited token or grant does none of it.
CREATE POLICY oauth_grant_seen_by_owner ON oauth_grant AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_token_whole(current_actor_id()) AND (user_id = current_actor_id() OR current_actor_administers()));
CREATE POLICY oauth_grant_made_by_owner ON oauth_grant AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_token_whole(current_actor_id()) AND user_id = current_actor_id() AND last_used_at IS NULL);
CREATE POLICY oauth_grant_ended_by_owner ON oauth_grant AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_token_whole(current_actor_id()) AND (user_id = current_actor_id() OR current_actor_administers()));
CREATE POLICY oauth_grant_project_seen_with_grant ON oauth_grant_project AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_token_whole(current_actor_id()) AND EXISTS (SELECT 1 FROM oauth_grant g WHERE g.id = oauth_grant_project.grant_id));
CREATE POLICY oauth_grant_project_added_with_grant ON oauth_grant_project AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_token_whole(current_actor_id()) AND oauth_grant_project_addable(grant_id, project_id));

REVOKE ALL ON oauth_client, oauth_request, oauth_grant, oauth_grant_project, oauth_code, oauth_token FROM exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_client, oauth_request, oauth_grant, oauth_grant_project, oauth_code, oauth_token
    TO exciter_admin;
GRANT SELECT (id, kind, name, uri) ON oauth_client TO exciter_app;
GRANT SELECT, INSERT, DELETE ON oauth_grant TO exciter_app;
GRANT SELECT, INSERT ON oauth_grant_project TO exciter_app;
-- The consent writes the code it hands the browser, which the token endpoint alone reads back, as the admin role.
GRANT INSERT ON oauth_code TO exciter_app;
REVOKE ALL ON FUNCTION oauth_grant_project_addable(uuid, uuid), oauth_request_take(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION oauth_grant_project_addable(uuid, uuid), oauth_request_take(uuid) TO exciter_app;

-- +goose Down
DROP TRIGGER IF EXISTS app_user_ends_grants ON app_user;
-- The function answers the request's rows, so it goes before their table.
DROP FUNCTION IF EXISTS oauth_request_take(uuid);
DROP TABLE IF EXISTS oauth_token, oauth_code, oauth_grant_project, oauth_grant, oauth_request, oauth_client;
DROP FUNCTION IF EXISTS oauth_grant_project_addable(uuid, uuid), oauth_grant_fixed(), oauth_grant_ended(), app_user_ends_grants();
ALTER TABLE installation DROP COLUMN IF EXISTS oauth_connections;
