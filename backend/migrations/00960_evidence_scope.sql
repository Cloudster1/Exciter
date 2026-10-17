-- +goose Up
-- The evidence scope (#200), as docs/api-contract-m9.md agrees: a personal token or an app's grant that uploads
-- test results and code scans and runs the trace check, and nothing else. The api holds the routes; the database
-- holds what a token and a grant may be.

ALTER TABLE api_token DROP CONSTRAINT api_token_known_scopes,
    ADD CONSTRAINT api_token_known_scopes CHECK (scopes <@ ARRAY['read', 'evidence']::text[] AND cardinality(scopes) <= 1);

ALTER TABLE oauth_request ADD COLUMN evidence boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT oauth_request_one_scope CHECK (NOT (read_only AND evidence));
ALTER TABLE oauth_grant ADD COLUMN evidence boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT oauth_grant_one_scope CHECK (NOT (read_only AND evidence));

-- +goose StatementBegin
-- What a grant is stays what it was made, its scope included: only its use is noted.
CREATE OR REPLACE FUNCTION oauth_grant_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.user_id, NEW.client_id, NEW.redirect_uri, NEW.read_only, NEW.evidence, NEW.projects_only, NEW.created_at)
       IS DISTINCT FROM (OLD.user_id, OLD.client_id, OLD.redirect_uri, OLD.read_only, OLD.evidence, OLD.projects_only, OLD.created_at) THEN
        RAISE EXCEPTION 'oauth_grant_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A grant stays what it was made; connect the client again instead.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION oauth_grant_fixed() RETURNS trigger
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
-- +goose StatementEnd
DELETE FROM oauth_grant WHERE evidence;
DELETE FROM oauth_request WHERE evidence;
ALTER TABLE oauth_grant DROP CONSTRAINT IF EXISTS oauth_grant_one_scope, DROP COLUMN IF EXISTS evidence;
ALTER TABLE oauth_request DROP CONSTRAINT IF EXISTS oauth_request_one_scope, DROP COLUMN IF EXISTS evidence;
DELETE FROM api_token WHERE 'evidence' = ANY (scopes);
ALTER TABLE api_token DROP CONSTRAINT api_token_known_scopes,
    ADD CONSTRAINT api_token_known_scopes CHECK (scopes <@ ARRAY['read']::text[]);
