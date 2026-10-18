-- +goose Up
-- Duplicates (#208), as docs/api-contract-m10.md agrees: requirements are compared by the trigrams of their title
-- and statement; the database keeps each requirement's most alike neighbours as its revisions are written, and what
-- people decided about a pair, which holds while both sides stay as they were.

-- A trusted extension, created by the role that migrates as unaccent was (#20).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- duplicates.MaxTextChars of what a requirement says, as the comparison reads it.
ALTER TABLE requirement ADD COLUMN similarity_text text
    GENERATED ALWAYS AS (left(title || ' ' || requirement_plain_text(statement), 2000)) STORED;
CREATE INDEX requirement_similarity_idx ON requirement USING gin (similarity_text gin_trgm_ops) WHERE kind = 'requirement';

-- Two requirements of a project alike at least duplicates.StoredThreshold, written by the database alone.
CREATE TABLE requirement_similarity (
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    low_id     uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    high_id    uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    score      real NOT NULL CHECK (score > 0 AND score <= 1),
    PRIMARY KEY (low_id, high_id),
    CONSTRAINT requirement_similarity_ordered CHECK (low_id < high_id)
);
CREATE INDEX requirement_similarity_project_idx ON requirement_similarity (project_id, score DESC);
CREATE INDEX requirement_similarity_high_idx ON requirement_similarity (high_id);

-- What somebody decided about a pair, at the revisions both sides had; only ever added to.
CREATE TABLE duplicate_decision (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    low_id     uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    high_id    uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    low_rev    integer NOT NULL CHECK (low_rev >= 1),
    high_rev   integer NOT NULL CHECK (high_rev >= 1),
    kind       text NOT NULL CHECK (kind IN ('dismissed', 'linked', 'merged')),
    -- duplicates.MaxReasonChars.
    reason     text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
    link_id    uuid REFERENCES trace_link(id) ON DELETE SET NULL,
    kept_id    uuid REFERENCES requirement(id) ON DELETE SET NULL,
    decided_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    decided_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT duplicate_decision_ordered CHECK (low_id < high_id),
    CONSTRAINT duplicate_decision_shape CHECK (
        (kind = 'dismissed' AND btrim(reason) <> '' AND link_id IS NULL AND kept_id IS NULL)
        OR (kind = 'linked' AND kept_id IS NULL)
        OR (kind = 'merged' AND link_id IS NULL AND kept_id IN (low_id, high_id)))
);
CREATE INDEX duplicate_decision_pair_idx ON duplicate_decision (low_id, high_id, decided_at DESC);

-- How alike two requirements of a project must be to be listed; without a row, duplicates.DefaultThreshold.
CREATE TABLE duplicate_settings (
    project_id uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    threshold  real NOT NULL CHECK (threshold BETWEEN 0.30 AND 0.95),
    updated_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
-- Keeps a requirement's most alike neighbours in its project, as it has just been written: its pairs before go, and
-- those with the duplicates.Neighbours most alike come, each at least duplicates.StoredThreshold alike.
CREATE FUNCTION requirement_similarity_refresh(target uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    r requirement%ROWTYPE;
BEGIN
    DELETE FROM requirement_similarity WHERE low_id = target OR high_id = target;
    SELECT * INTO r FROM requirement WHERE id = target;
    IF NOT FOUND OR r.kind <> 'requirement' OR r.trashed_at IS NOT NULL OR btrim(r.similarity_text) = '' THEN
        RETURN;
    END IF;
    PERFORM set_config('pg_trgm.similarity_threshold', '0.30', true);
    INSERT INTO requirement_similarity (project_id, low_id, high_id, score)
    SELECT r.project_id, least(r.id, o.id), greatest(r.id, o.id), similarity(r.similarity_text, o.similarity_text)
    FROM requirement o
    WHERE o.project_id = r.project_id AND o.kind = 'requirement' AND o.trashed_at IS NULL AND o.id <> r.id
      AND o.similarity_text % r.similarity_text
    ORDER BY similarity(r.similarity_text, o.similarity_text) DESC, o.id
    LIMIT 20
    ON CONFLICT (low_id, high_id) DO UPDATE SET score = EXCLUDED.score;
END;
$$;

-- Every content change writes a revision, so the neighbours follow each one.
CREATE FUNCTION requirement_similarity_follows() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM requirement_similarity_refresh(NEW.requirement_id);
    RETURN NULL;
END;
$$;

-- Whether a decision about a pair still holds: a dismissal while both sides stay at the revisions it saw, a link
-- while it lasts.
CREATE FUNCTION duplicate_pair_settled(low uuid, high uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM duplicate_decision d
        JOIN requirement a ON a.id = d.low_id JOIN requirement b ON b.id = d.high_id
        WHERE d.low_id = low AND d.high_id = high
          AND ((d.kind = 'dismissed' AND d.low_rev = a.revision AND d.high_rev = b.revision)
               OR (d.kind = 'linked' AND EXISTS (SELECT 1 FROM trace_link l WHERE l.id = d.link_id AND l.deleted_at IS NULL))))
$$;

-- A decision names two requirements of its project at revisions they had; a merge's side that went is in the trash
-- and the side kept is not, and a link joins the two.
CREATE FUNCTION duplicate_decision_holds() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    low requirement%ROWTYPE;
    high requirement%ROWTYPE;
BEGIN
    SELECT * INTO low FROM requirement WHERE id = NEW.low_id;
    SELECT * INTO high FROM requirement WHERE id = NEW.high_id;
    IF low.project_id IS DISTINCT FROM NEW.project_id OR high.project_id IS DISTINCT FROM NEW.project_id
       OR low.kind <> 'requirement' OR high.kind <> 'requirement'
       OR NEW.low_rev > low.revision OR NEW.high_rev > high.revision
       OR (NEW.kind = 'merged' AND ((NEW.kept_id = NEW.low_id AND (low.trashed_at IS NOT NULL OR high.trashed_at IS NULL))
                                    OR (NEW.kept_id = NEW.high_id AND (high.trashed_at IS NOT NULL OR low.trashed_at IS NULL))))
       OR (NEW.kind = 'linked' AND NOT EXISTS (
               SELECT 1 FROM trace_link l WHERE l.id = NEW.link_id AND l.deleted_at IS NULL
               AND ((l.source_id, l.target_id) = (NEW.low_id, NEW.high_id) OR (l.source_id, l.target_id) = (NEW.high_id, NEW.low_id)))) THEN
        RAISE EXCEPTION 'duplicate_decision_holds' USING ERRCODE = 'check_violation', CONSTRAINT = 'duplicate_decision_holds',
            DETAIL = 'A decision names two requirements of its project as they are.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_similarity_follows AFTER INSERT ON requirement_revision
    FOR EACH ROW EXECUTE FUNCTION requirement_similarity_follows();
CREATE TRIGGER duplicate_decision_holds BEFORE INSERT ON duplicate_decision
    FOR EACH ROW EXECUTE FUNCTION duplicate_decision_holds();

ALTER TABLE requirement_similarity ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_similarity FORCE  ROW LEVEL SECURITY;
ALTER TABLE duplicate_decision     ENABLE ROW LEVEL SECURITY;
ALTER TABLE duplicate_decision     FORCE  ROW LEVEL SECURITY;
ALTER TABLE duplicate_settings     ENABLE ROW LEVEL SECURITY;
ALTER TABLE duplicate_settings     FORCE  ROW LEVEL SECURITY;

CREATE POLICY requirement_similarity_installation ON requirement_similarity USING (true) WITH CHECK (true);
CREATE POLICY duplicate_decision_installation ON duplicate_decision USING (true) WITH CHECK (true);
CREATE POLICY duplicate_settings_installation ON duplicate_settings USING (true) WITH CHECK (true);

-- Whoever views a project reads its pairs, its decisions and its threshold; its editors of an open project decide
-- in their own name, and its administrators set the threshold.
CREATE POLICY requirement_similarity_readers ON requirement_similarity AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY duplicate_decision_readers ON duplicate_decision AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY duplicate_decision_deciders ON duplicate_decision AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id)
                AND decided_by = current_actor_id());
CREATE POLICY duplicate_settings_readers ON duplicate_settings AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY duplicate_settings_keepers ON duplicate_settings AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id)
                AND updated_by = current_actor_id());
CREATE POLICY duplicate_settings_changers ON duplicate_settings AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND updated_by = current_actor_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON requirement_similarity, duplicate_decision, duplicate_settings TO exciter_admin;
REVOKE ALL ON requirement_similarity, duplicate_decision, duplicate_settings FROM exciter_app;
GRANT SELECT ON requirement_similarity TO exciter_app;
GRANT SELECT, INSERT ON duplicate_decision, duplicate_settings TO exciter_app;
GRANT UPDATE (threshold, updated_by, updated_at) ON duplicate_settings TO exciter_app;
REVOKE ALL ON FUNCTION requirement_similarity_refresh(uuid), duplicate_pair_settled(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION duplicate_pair_settled(uuid, uuid) TO exciter_app, exciter_admin;

-- The requirements there are find their neighbours once.
SELECT requirement_similarity_refresh(id) FROM requirement WHERE kind = 'requirement' AND trashed_at IS NULL;

-- +goose Down
DROP TRIGGER requirement_similarity_follows ON requirement_revision;
DROP TABLE duplicate_settings, duplicate_decision, requirement_similarity;
DROP FUNCTION requirement_similarity_follows(), requirement_similarity_refresh(uuid), duplicate_pair_settled(uuid, uuid),
    duplicate_decision_holds();
DROP INDEX requirement_similarity_idx;
ALTER TABLE requirement DROP COLUMN similarity_text;
DROP EXTENSION IF EXISTS pg_trgm;
