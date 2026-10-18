-- +goose Up
-- The glossary (#210), as docs/api-contract-m10.md agrees: a project's terms, each defined once with its synonyms and
-- the words not to use instead, kept in revisions; the library glossaries a project uses; and the terms each
-- requirement uses, which turn suspect for its owner once a definition changes, as a link does.

-- +goose StatementBegin
-- A term's words as the search reads them; array_to_string is stable only for its types' output, which text keeps.
CREATE FUNCTION glossary_words(name text, synonyms text[]) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT name || ' ' || array_to_string(synonyms, ' ')
$$;

-- Whether a term's word lists hold: at most glossary.MaxSynonyms or MaxAvoid words, each 1 to glossary.MaxNameLength
-- characters on one line.
CREATE FUNCTION glossary_words_fit(words text[], most integer) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT cardinality(words) <= most
       AND NOT EXISTS (SELECT 1 FROM unnest(words) w WHERE btrim(w) = '' OR char_length(w) > 120 OR w ~ '[\r\n]')
$$;
-- +goose StatementEnd

CREATE TABLE glossary_term (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id          uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- glossary.MaxNameLength.
    name                citext NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 120 AND name !~ '[\r\n]'),
    definition          jsonb NOT NULL CHECK (jsonb_typeof(definition) = 'object'),
    synonyms            text[] NOT NULL DEFAULT '{}' CHECK (glossary_words_fit(synonyms, 20)),
    avoid               text[] NOT NULL DEFAULT '{}' CHECK (glossary_words_fit(avoid, 20)),
    status              text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'approved', 'deprecated')),
    -- Every change is a revision; definition_revision is the latest that changed the name or the definition.
    revision            integer NOT NULL DEFAULT 1 CHECK (revision >= 1),
    definition_revision integer NOT NULL DEFAULT 1 CHECK (definition_revision BETWEEN 1 AND revision),
    -- What the change that made this revision says of itself, carried into the revision.
    comment             text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    search_vector       tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('exciter_search', glossary_words(name::text, synonyms)), 'A')
        || setweight(to_tsvector('exciter_search', left(requirement_plain_text(definition), 200000)), 'B')) STORED,
    created_by          uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_by          uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT glossary_term_name_once UNIQUE (project_id, name)
);
CREATE INDEX glossary_term_search_idx ON glossary_term USING gin (search_vector);

-- Every revision of a term, written by the database as the term changes and never after.
CREATE TABLE glossary_term_revision (
    term_id    uuid NOT NULL REFERENCES glossary_term(id) ON DELETE CASCADE,
    number     integer NOT NULL CHECK (number >= 1),
    name       text NOT NULL,
    definition jsonb NOT NULL,
    synonyms   text[] NOT NULL,
    avoid      text[] NOT NULL,
    status     text NOT NULL,
    comment    text NOT NULL,
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (term_id, number)
);

-- The library projects whose glossaries a project uses, in order.
CREATE TABLE project_glossary (
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    library_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    position   integer NOT NULL CHECK (position >= 0),
    added_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    added_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, library_id),
    CONSTRAINT project_glossary_elsewhere CHECK (project_id <> library_id)
);

-- A term the current revision of a requirement uses, and the term's revision its owner last confirmed it under.
CREATE TABLE term_use (
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    term_id        uuid NOT NULL REFERENCES glossary_term(id) ON DELETE CASCADE,
    -- An author linked words to the term, rather than the words naming it.
    explicit       boolean NOT NULL,
    seen_revision  integer NOT NULL CHECK (seen_revision >= 1),
    PRIMARY KEY (requirement_id, term_id)
);
CREATE INDEX term_use_term_idx ON term_use (term_id);

-- A confirmation that a requirement holds under a term's changed definition.
CREATE TABLE term_use_clearance (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    requirement_id       uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    term_id              uuid NOT NULL REFERENCES glossary_term(id) ON DELETE CASCADE,
    term_revision        integer NOT NULL CHECK (term_revision >= 1),
    requirement_revision integer NOT NULL CHECK (requirement_revision >= 1),
    comment              text NOT NULL CHECK (btrim(comment) <> '' AND char_length(comment) <= 500),
    cleared_by           uuid REFERENCES app_user(id) ON DELETE SET NULL,
    cleared_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX term_use_clearance_idx ON term_use_clearance (requirement_id, term_id, cleared_at DESC);

-- A word the glossary says not to use is a finding of the writing rules, which names the term to use instead.
ALTER TABLE quality_finding ADD COLUMN use_instead text NOT NULL DEFAULT '' CHECK (char_length(use_instead) <= 200);
INSERT INTO quality_rule (rule, default_severity, default_enabled) VALUES ('avoid_term', 'warning', true)
    ON CONFLICT (rule) DO NOTHING;

-- A notification of a changed definition names its term beside the requirement.
ALTER TABLE notification ADD COLUMN term_id uuid REFERENCES glossary_term(id) ON DELETE CASCADE,
    DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed', 'code_outdated', 'code_orphaned', 'term_changed'));

-- +goose StatementBegin
-- No two terms of a glossary share a word, name or synonym, whatever the case; a project holds at most
-- glossary.MaxTerms terms.
CREATE FUNCTION glossary_term_words_free() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    words text[] := ARRAY(SELECT DISTINCT lower(btrim(w)) FROM unnest(array_append(NEW.synonyms, NEW.name::text)) w);
BEGIN
    IF cardinality(words) <> 1 + cardinality(NEW.synonyms) THEN
        RAISE EXCEPTION 'glossary_term_words' USING ERRCODE = 'unique_violation', CONSTRAINT = 'glossary_term_words',
            DETAIL = 'A term names each of its words once.';
    END IF;
    IF EXISTS (SELECT 1 FROM glossary_term t, unnest(array_append(t.synonyms, t.name::text)) w
               WHERE t.project_id = NEW.project_id AND t.id <> NEW.id AND lower(btrim(w)) = ANY (words)) THEN
        RAISE EXCEPTION 'glossary_term_words' USING ERRCODE = 'unique_violation', CONSTRAINT = 'glossary_term_words',
            DETAIL = 'Another term of the glossary has that word.';
    END IF;
    IF TG_OP = 'INSERT' AND (SELECT count(*) FROM glossary_term WHERE project_id = NEW.project_id) >= 5000 THEN
        RAISE EXCEPTION 'glossary_term_limit' USING ERRCODE = 'check_violation', CONSTRAINT = 'glossary_term_limit',
            DETAIL = 'A glossary holds at most 5000 terms.';
    END IF;
    RETURN NEW;
END;
$$;

-- A change is the next revision: its number counts on, the definition's revision follows a new name or definition,
-- and what made a term stays as it was.
CREATE FUNCTION glossary_term_next() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.revision := 1;
        NEW.definition_revision := 1;
        NEW.updated_by := NEW.created_by;
        RETURN NEW;
    END IF;
    IF (NEW.id, NEW.project_id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.id, OLD.project_id, OLD.created_by, OLD.created_at) THEN
        RAISE EXCEPTION 'glossary_term_fixed' USING ERRCODE = 'check_violation', CONSTRAINT = 'glossary_term_fixed',
            DETAIL = 'A term stays in its project, made by whoever made it.';
    END IF;
    NEW.revision := OLD.revision + 1;
    NEW.updated_at := now();
    NEW.definition_revision := CASE WHEN (NEW.name, NEW.definition) IS DISTINCT FROM (OLD.name, OLD.definition)
                                    THEN NEW.revision ELSE OLD.definition_revision END;
    RETURN NEW;
END;
$$;

-- Every state of a term is kept as its revision, written as the definer, for nobody writes revisions by hand.
CREATE FUNCTION glossary_term_revised() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO glossary_term_revision (term_id, number, name, definition, synonyms, avoid, status, comment, created_by)
    VALUES (NEW.id, NEW.revision, NEW.name, NEW.definition, NEW.synonyms, NEW.avoid, NEW.status, NEW.comment, NEW.updated_by);
    RETURN NULL;
END;
$$;

-- A project uses a library project's glossary, another project, which whoever adds it may read.
CREATE FUNCTION project_glossary_library() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM project p WHERE p.id = NEW.library_id AND COALESCE((p.settings ->> 'library')::boolean, false))
       OR NOT perm_project_holds(NEW.library_id, current_actor_id(), 'view') THEN
        RAISE EXCEPTION 'project_glossary_library' USING ERRCODE = 'check_violation', CONSTRAINT = 'project_glossary_library',
            DETAIL = 'A project uses the glossaries of library projects its administrator may read.';
    END IF;
    RETURN NEW;
END;
$$;

-- A use is written seeing the term's current definition, whatever the writer says. Only term_use_clear moves what it
-- has seen, since the app may update nothing but whether it is explicit.
CREATE FUNCTION term_use_seen() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        SELECT definition_revision INTO NEW.seen_revision FROM glossary_term WHERE id = NEW.term_id;
    ELSIF (NEW.requirement_id, NEW.term_id) IS DISTINCT FROM (OLD.requirement_id, OLD.term_id) THEN
        RAISE EXCEPTION 'term_use_fixed' USING ERRCODE = 'check_violation', CONSTRAINT = 'term_use_fixed',
            DETAIL = 'A use stays the use of its requirement and its term.';
    END IF;
    RETURN NEW;
END;
$$;

-- The terms that mark a project's requirements, as the reader may read them: its own first, then each library's.
CREATE FUNCTION glossary_terms_of(project uuid) RETURNS TABLE (term_id uuid, rank integer)
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT t.id, 0 FROM glossary_term t WHERE t.project_id = project
    UNION ALL
    SELECT t.id, g.position + 1 FROM project_glossary g JOIN glossary_term t ON t.project_id = g.library_id
    WHERE g.project_id = project
$$;

-- The projects that use a library's glossary, which its terms' writers may not read, for their writing rules to
-- check again when the words change.
CREATE FUNCTION glossary_users(library uuid) RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT g.project_id FROM project_glossary g WHERE g.library_id = library
$$;

-- Confirms that a requirement holds under the term's current definition: by whoever may edit it, naming the term's
-- revision they read, with a comment the record keeps.
CREATE FUNCTION term_use_clear(requirement uuid, term uuid, term_revision integer, note text) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    current integer;
    rev integer;
BEGIN
    IF actor IS NULL OR NOT perm_requirement_editable(requirement, actor)
       OR NOT EXISTS (SELECT 1 FROM requirement r WHERE r.id = requirement AND r.trashed_at IS NULL AND project_open(r.project_id)) THEN
        RAISE EXCEPTION 'term_use_clear_forbidden' USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'term_use_clear_forbidden',
            DETAIL = 'A use is confirmed by whoever may edit its requirement.';
    END IF;
    SELECT t.definition_revision INTO current FROM term_use u JOIN glossary_term t ON t.id = u.term_id
    WHERE u.requirement_id = requirement AND u.term_id = term;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'term_use_missing' USING ERRCODE = 'no_data_found', DETAIL = 'The requirement does not use that term.';
    END IF;
    IF current <> term_revision THEN
        RAISE EXCEPTION 'term_use_stale' USING ERRCODE = 'serialization_failure', CONSTRAINT = 'term_use_stale',
            DETAIL = 'The term changed since it was read.';
    END IF;
    SELECT r.revision INTO rev FROM requirement r WHERE r.id = requirement;
    UPDATE term_use SET seen_revision = current WHERE requirement_id = requirement AND term_id = term;
    INSERT INTO term_use_clearance (requirement_id, term_id, term_revision, requirement_revision, comment, cleared_by)
    VALUES (requirement, term, current, rev, note, actor);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER glossary_term_words_free BEFORE INSERT OR UPDATE ON glossary_term
    FOR EACH ROW EXECUTE FUNCTION glossary_term_words_free();
CREATE TRIGGER glossary_term_next BEFORE INSERT OR UPDATE ON glossary_term FOR EACH ROW EXECUTE FUNCTION glossary_term_next();
CREATE TRIGGER glossary_term_revised AFTER INSERT OR UPDATE ON glossary_term FOR EACH ROW EXECUTE FUNCTION glossary_term_revised();
CREATE TRIGGER project_glossary_library BEFORE INSERT ON project_glossary FOR EACH ROW EXECUTE FUNCTION project_glossary_library();
CREATE TRIGGER term_use_seen BEFORE INSERT OR UPDATE ON term_use FOR EACH ROW EXECUTE FUNCTION term_use_seen();

ALTER TABLE glossary_term          ENABLE ROW LEVEL SECURITY;
ALTER TABLE glossary_term          FORCE  ROW LEVEL SECURITY;
ALTER TABLE glossary_term_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE glossary_term_revision FORCE  ROW LEVEL SECURITY;
ALTER TABLE project_glossary       ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_glossary       FORCE  ROW LEVEL SECURITY;
ALTER TABLE term_use               ENABLE ROW LEVEL SECURITY;
ALTER TABLE term_use               FORCE  ROW LEVEL SECURITY;
ALTER TABLE term_use_clearance     ENABLE ROW LEVEL SECURITY;
ALTER TABLE term_use_clearance     FORCE  ROW LEVEL SECURITY;

CREATE POLICY glossary_term_installation ON glossary_term USING (true) WITH CHECK (true);
CREATE POLICY glossary_term_revision_installation ON glossary_term_revision USING (true) WITH CHECK (true);
CREATE POLICY project_glossary_installation ON project_glossary USING (true) WITH CHECK (true);
CREATE POLICY term_use_installation ON term_use USING (true) WITH CHECK (true);
CREATE POLICY term_use_clearance_installation ON term_use_clearance USING (true) WITH CHECK (true);

-- Whoever views a project reads its glossary; its editors of an open project define and change terms, in their own
-- name, and its administrators delete them.
CREATE POLICY glossary_term_readers ON glossary_term AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY glossary_term_makers ON glossary_term AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id)
                AND created_by = current_actor_id());
CREATE POLICY glossary_term_changers ON glossary_term AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id))
    WITH CHECK (updated_by = current_actor_id());
CREATE POLICY glossary_term_removers ON glossary_term AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));
CREATE POLICY glossary_term_revision_readers ON glossary_term_revision AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id));

-- Whoever views a project reads which glossaries it uses; its administrators of an open project choose them.
CREATE POLICY project_glossary_readers ON project_glossary AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY project_glossary_choosers ON project_glossary AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id)
                AND added_by = current_actor_id());
CREATE POLICY project_glossary_droppers ON project_glossary AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));

-- A use shows to whoever reads both its requirement and its term; it is written by whoever may edit the requirement.
CREATE POLICY term_use_readers ON term_use AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()) AND EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id));
CREATE POLICY term_use_writers ON term_use AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY term_use_changers ON term_use AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()))
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY term_use_removers ON term_use AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY term_use_clearance_readers ON term_use_clearance AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()) AND EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id));

-- A notification of a term is for whoever may read its requirement and the term.
DROP POLICY notification_own_read ON notification;
DROP POLICY notification_own_write ON notification;
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id())
         AND (term_id IS NULL OR EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id)))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id())
         AND (term_id IS NULL OR EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id)))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));

GRANT SELECT, INSERT, UPDATE, DELETE ON glossary_term, glossary_term_revision, project_glossary, term_use, term_use_clearance
    TO exciter_admin;
REVOKE ALL ON glossary_term, glossary_term_revision, project_glossary, term_use, term_use_clearance FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON glossary_term, project_glossary, term_use TO exciter_app;
GRANT UPDATE (name, definition, synonyms, avoid, status, comment, updated_by) ON glossary_term TO exciter_app;
GRANT UPDATE (explicit) ON term_use TO exciter_app;
GRANT SELECT ON glossary_term_revision, term_use_clearance TO exciter_app;
REVOKE ALL ON FUNCTION term_use_clear(uuid, uuid, integer, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION term_use_clear(uuid, uuid, integer, text) TO exciter_app, exciter_admin;
REVOKE ALL ON FUNCTION glossary_users(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION glossary_users(uuid) TO exciter_app, exciter_admin;

-- +goose Down
DROP POLICY notification_own_read ON notification;
DROP POLICY notification_own_write ON notification;
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));
DELETE FROM notification WHERE kind = 'term_changed';
ALTER TABLE notification DROP COLUMN term_id,
    DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed', 'code_outdated', 'code_orphaned'));
ALTER TABLE quality_finding DROP COLUMN use_instead;
DROP TABLE term_use_clearance, term_use, project_glossary, glossary_term_revision, glossary_term;
DROP FUNCTION term_use_clear(uuid, uuid, integer, text), glossary_users(uuid), glossary_terms_of(uuid), term_use_seen(), project_glossary_library(),
    glossary_term_revised(), glossary_term_next(), glossary_term_words_free(), glossary_words_fit(text[], integer),
    glossary_words(text, text[]);
