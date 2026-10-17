-- +goose Up
-- What an assistant wrote (#190), as docs/api-contract-m7.md agrees: a row written through a personal token or an
-- OAuth grant carries which, and its name at that moment, so a reader tells what a person wrote from what their
-- assistant wrote for them. The database writes it from the transaction's app.via, never a caller.

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['requirement_revision', 'requirement_change', 'comment', 'requirement_review', 'trace_link',
                             'armature_issue_link', 'stator_page_link', 'audit_log'] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN via_kind text CHECK (via_kind IN (''token'', ''oauth''))', t);
        -- No key: a token revoked or a grant ended leaves what it wrote, under the name it had.
        EXECUTE format('ALTER TABLE %I ADD COLUMN via_id uuid', t);
        EXECUTE format('ALTER TABLE %I ADD COLUMN via_name text', t);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((via_kind IS NULL) = (via_id IS NULL) AND (via_kind IS NULL) = (via_name IS NULL))',
                       t, t || '_via_whole');
    END LOOP;
END $$;

-- The token or grant the transaction's app.via names, when the acting person holds it, read past row level
-- security, since a token limited to projects may not read tokens; no row for a session.
CREATE FUNCTION via_held() RETURNS TABLE (kind text, id uuid, name text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH said AS (SELECT NULLIF(current_setting('app.via', true), '')::uuid AS id)
    SELECT 'token', t.id, t.name FROM api_token t, said WHERE t.id = said.id AND t.user_id = current_actor_id()
    UNION ALL
    SELECT 'oauth', g.id, c.name FROM oauth_grant g JOIN oauth_client c ON c.id = g.client_id, said
    WHERE g.id = said.id AND g.user_id = current_actor_id()
    LIMIT 1
$$;

-- Stamps a row with what via_held names; a value sent by hand is overwritten, and one is never changed.
CREATE FUNCTION via_stamped() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF (NEW.via_kind, NEW.via_id, NEW.via_name) IS DISTINCT FROM (OLD.via_kind, OLD.via_id, OLD.via_name) THEN
            RAISE EXCEPTION 'via_fixed' USING ERRCODE = 'check_violation',
                DETAIL = 'What a row was written through stays as it was written.';
        END IF;
        RETURN NEW;
    END IF;
    NEW.via_kind := NULL;
    NEW.via_id := NULL;
    NEW.via_name := NULL;
    SELECT h.kind, h.id, h.name INTO NEW.via_kind, NEW.via_id, NEW.via_name FROM via_held() h;
    RETURN NEW;
END;
$$;

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['requirement_revision', 'requirement_change', 'comment', 'requirement_review', 'trace_link',
                             'armature_issue_link', 'stator_page_link', 'audit_log'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION via_stamped()', t || '_via', t);
    END LOOP;
END $$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION via_stamped(), via_held() FROM PUBLIC;
-- An event names what it was written through too, as Emit writes it in the change's transaction.
GRANT EXECUTE ON FUNCTION via_held() TO exciter_app, exciter_admin;

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['requirement_revision', 'requirement_change', 'comment', 'requirement_review', 'trace_link',
                             'armature_issue_link', 'stator_page_link', 'audit_log'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', t || '_via', t);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I, DROP COLUMN IF EXISTS via_kind, DROP COLUMN IF EXISTS via_id, DROP COLUMN IF EXISTS via_name',
                       t, t || '_via_whole');
    END LOOP;
END $$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS via_stamped();
DROP FUNCTION IF EXISTS via_held();
