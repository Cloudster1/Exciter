-- +goose Up
-- Writing rules (#207), as docs/api-contract-m10.md agrees: every revision keeps what each rule found in its title and
-- statement, with the word lists of the time; whether a rule is on, how much it weighs, which types it applies to and
-- what an author accepted are judged when the score is read, so a changed setting takes effect at once everywhere.

-- A project names the language its requirements are written in, which picks the rules' word lists.
ALTER TABLE project ADD CONSTRAINT project_language CHECK (settings->>'language' IS NULL OR settings->>'language' IN ('en', 'de'));

-- The rules there are and how each starts out in a project, kept equal to quality.Rules by a test.
CREATE TABLE quality_rule (
    rule             text PRIMARY KEY CHECK (rule ~ '^[a-z]+(_[a-z]+)*$'),
    default_severity text NOT NULL CHECK (default_severity IN ('error', 'warning', 'hint')),
    default_enabled  boolean NOT NULL
);
INSERT INTO quality_rule (rule, default_severity, default_enabled) VALUES
    ('vague', 'warning', true), ('unbounded', 'warning', true), ('escape', 'error', true), ('placeholder', 'error', true),
    ('and_or', 'warning', true), ('multiple_shall', 'warning', true), ('negative', 'hint', true), ('passive', 'warning', true),
    ('pronoun', 'warning', true), ('ears', 'hint', true), ('avoid_term', 'warning', true);

-- +goose StatementBegin
-- Whether words fit a rule's list: at most quality.MaxCustomWords, each 1 to quality.MaxWordLength characters on
-- one line.
CREATE FUNCTION quality_words_fit(words text[]) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT cardinality(words) <= 200
       AND NOT EXISTS (SELECT 1 FROM unnest(words) w WHERE btrim(w) = '' OR char_length(w) > 60 OR w ~ '[\r\n]')
$$;

-- What a severity takes from the score, as quality.Weight says.
CREATE FUNCTION quality_weight(severity text) RETURNS integer
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE severity WHEN 'error' THEN 20 WHEN 'warning' THEN 7 ELSE 2 END
$$;
-- +goose StatementEnd

-- How a project uses a rule; without a row, as the rule starts out. Empty type_ids is every type.
CREATE TABLE quality_rule_setting (
    project_id   uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    rule         text NOT NULL REFERENCES quality_rule(rule) ON DELETE CASCADE,
    enabled      boolean NOT NULL,
    severity     text NOT NULL CHECK (severity IN ('error', 'warning', 'hint')),
    type_ids     uuid[] NOT NULL DEFAULT '{}',
    -- What the project adds to the rule's built-in list, and the built-in words it does not want found.
    add_words    text[] NOT NULL DEFAULT '{}' CHECK (quality_words_fit(add_words)),
    except_words text[] NOT NULL DEFAULT '{}' CHECK (quality_words_fit(except_words)),
    updated_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, rule),
    CONSTRAINT quality_rule_setting_words CHECK ((cardinality(add_words) = 0 AND cardinality(except_words) = 0)
                                                 OR rule IN ('vague', 'unbounded', 'escape', 'placeholder'))
);

-- What the project's findings depend on beyond the text, as quality.ConfigHash names it, written by the worker as it
-- checks again; a requirement checked under another is stale until the worker reaches it.
CREATE TABLE project_quality (
    project_id uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    config     text NOT NULL CHECK (config ~ '^[0-9a-f]{64}$'),
    changed_at timestamptz NOT NULL DEFAULT now()
);

-- That a revision was checked, and under what.
CREATE TABLE requirement_quality (
    requirement_id uuid NOT NULL,
    revision       integer NOT NULL,
    config         text NOT NULL CHECK (config ~ '^[0-9a-f]{64}$'),
    checked_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (requirement_id, revision),
    FOREIGN KEY (requirement_id, revision) REFERENCES requirement_revision(requirement_id, number) ON DELETE CASCADE
);

-- One place a rule found in a revision, switched on or not; key stays the same while the words do.
CREATE TABLE quality_finding (
    requirement_id uuid NOT NULL,
    revision       integer NOT NULL,
    key            text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 400),
    rule           text NOT NULL REFERENCES quality_rule(rule) ON DELETE CASCADE,
    field          text NOT NULL CHECK (field IN ('title', 'statement')),
    block          integer NOT NULL CHECK (block >= 0),
    start_at       integer NOT NULL CHECK (start_at >= 0),
    end_at         integer NOT NULL,
    -- quality.MaxMatchChars.
    match          text NOT NULL CHECK (char_length(match) <= 200),
    PRIMARY KEY (requirement_id, revision, key),
    FOREIGN KEY (requirement_id, revision) REFERENCES requirement_quality(requirement_id, revision) ON DELETE CASCADE,
    CONSTRAINT quality_finding_span CHECK (end_at >= start_at)
);

-- An author's word that a finding stays, for one revision; carried_from is the revision it was first given for.
CREATE TABLE quality_acceptance (
    requirement_id uuid NOT NULL,
    revision       integer NOT NULL,
    key            text NOT NULL,
    -- quality.MaxReasonChars.
    reason         text NOT NULL CHECK (btrim(reason) <> '' AND char_length(reason) <= 500),
    carried_from   integer CHECK (carried_from >= 1 AND carried_from < revision),
    accepted_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    accepted_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (requirement_id, revision, key),
    FOREIGN KEY (requirement_id, revision) REFERENCES requirement_revision(requirement_id, number) ON DELETE CASCADE
);

-- +goose StatementBegin
-- The rules as a project uses them for one type of requirement.
CREATE FUNCTION quality_rule_state(project uuid, type uuid) RETURNS TABLE (rule text, enabled boolean, severity text)
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT r.rule,
           COALESCE(s.enabled, r.default_enabled) AND (s.type_ids IS NULL OR cardinality(s.type_ids) = 0 OR type = ANY (s.type_ids)),
           COALESCE(s.severity, r.default_severity)
    FROM quality_rule r
    LEFT JOIN quality_rule_setting s ON s.project_id = project AND s.rule = r.rule
$$;

-- A requirement's quality as the reader sees it: its score (null for a section or one not checked yet), how many
-- findings count and how many were accepted, the rules that count, and whether a recheck has yet to reach it.
CREATE FUNCTION requirement_quality_of(target uuid)
    RETURNS TABLE (score integer, active integer, accepted integer, rules text[], stale boolean)
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    WITH q AS (
        SELECT r.id, r.project_id, r.type_id, r.revision, r.kind, c.config AS checked, pq.config AS wanted
        FROM requirement r
        LEFT JOIN requirement_quality c ON c.requirement_id = r.id AND c.revision = r.revision
        LEFT JOIN project_quality pq ON pq.project_id = r.project_id
        WHERE r.id = target
    ), f AS (
        SELECT f.rule, st.severity, st.enabled,
               EXISTS (SELECT 1 FROM quality_acceptance a WHERE a.requirement_id = f.requirement_id
                       AND a.revision = f.revision AND a.key = f.key) AS taken
        FROM q
        JOIN quality_finding f ON f.requirement_id = q.id AND f.revision = q.revision
        JOIN quality_rule_state(q.project_id, q.type_id) st ON st.rule = f.rule
    ), per_rule AS (
        SELECT rule, least(2 * quality_weight(min(severity)), count(*)::int * quality_weight(min(severity))) AS cost
        FROM f WHERE enabled AND NOT taken GROUP BY rule
    )
    SELECT CASE WHEN q.kind <> 'requirement' OR q.checked IS NULL THEN NULL
                ELSE greatest(0, 100 - COALESCE((SELECT sum(cost) FROM per_rule), 0))::int END,
           (SELECT count(*) FROM f WHERE enabled AND NOT taken)::int,
           (SELECT count(*) FROM f WHERE taken)::int,
           COALESCE((SELECT array_agg(rule ORDER BY rule) FROM per_rule), '{}'),
           q.kind = 'requirement' AND (q.checked IS NULL OR (q.wanted IS NOT NULL AND q.checked <> q.wanted))
    FROM q
$$;

-- The score of every requirement of a project at once, as requirement_quality_of scores one, for the tree and the
-- grid, which would otherwise score row by row.
CREATE FUNCTION project_quality_scores(project uuid) RETURNS TABLE (requirement_id uuid, score integer)
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    WITH r AS (
        SELECT q.id, q.type_id, q.revision FROM requirement q WHERE q.project_id = project AND q.kind = 'requirement'
    ), st AS (
        SELECT t.type_id, s.rule, s.enabled, s.severity
        FROM (SELECT DISTINCT r.type_id FROM r) t CROSS JOIN LATERAL quality_rule_state(project, t.type_id) s
    ), cost AS (
        SELECT f.requirement_id, least(2 * quality_weight(min(st.severity)), count(*)::int * quality_weight(min(st.severity))) AS cost
        FROM r
        JOIN quality_finding f ON f.requirement_id = r.id AND f.revision = r.revision
        JOIN st ON st.type_id IS NOT DISTINCT FROM r.type_id AND st.rule = f.rule
        WHERE st.enabled AND NOT EXISTS (SELECT 1 FROM quality_acceptance a WHERE a.requirement_id = f.requirement_id
                                         AND a.revision = f.revision AND a.key = f.key)
        GROUP BY f.requirement_id, f.rule
    )
    SELECT r.id, greatest(0, 100 - COALESCE((SELECT sum(c.cost) FROM cost c WHERE c.requirement_id = r.id), 0))::int
    FROM r JOIN requirement_quality c ON c.requirement_id = r.id AND c.revision = r.revision
$$;

-- An acceptance names a finding its revision has; one carried forward repeats the one of the revision before.
CREATE FUNCTION quality_acceptance_of_a_finding() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM quality_finding f WHERE f.requirement_id = NEW.requirement_id
                   AND f.revision = NEW.revision AND f.key = NEW.key) THEN
        RAISE EXCEPTION 'quality_acceptance_finding' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'quality_acceptance_finding',
            DETAIL = 'An acceptance names a finding of its revision.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER quality_acceptance_of_a_finding BEFORE INSERT ON quality_acceptance
    FOR EACH ROW EXECUTE FUNCTION quality_acceptance_of_a_finding();

ALTER TABLE quality_rule         ENABLE ROW LEVEL SECURITY;
ALTER TABLE quality_rule         FORCE  ROW LEVEL SECURITY;
ALTER TABLE quality_rule_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE quality_rule_setting FORCE  ROW LEVEL SECURITY;
ALTER TABLE project_quality      ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_quality      FORCE  ROW LEVEL SECURITY;
ALTER TABLE requirement_quality  ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_quality  FORCE  ROW LEVEL SECURITY;
ALTER TABLE quality_finding      ENABLE ROW LEVEL SECURITY;
ALTER TABLE quality_finding      FORCE  ROW LEVEL SECURITY;
ALTER TABLE quality_acceptance   ENABLE ROW LEVEL SECURITY;
ALTER TABLE quality_acceptance   FORCE  ROW LEVEL SECURITY;

CREATE POLICY quality_rule_installation ON quality_rule USING (true) WITH CHECK (true);
CREATE POLICY quality_rule_setting_installation ON quality_rule_setting USING (true) WITH CHECK (true);
CREATE POLICY project_quality_installation ON project_quality USING (true) WITH CHECK (true);
CREATE POLICY requirement_quality_installation ON requirement_quality USING (true) WITH CHECK (true);
CREATE POLICY quality_finding_installation ON quality_finding USING (true) WITH CHECK (true);
CREATE POLICY quality_acceptance_installation ON quality_acceptance USING (true) WITH CHECK (true);

-- Whoever views a project reads how it uses the rules; its administrators of an open project set them.
CREATE POLICY quality_rule_setting_readers ON quality_rule_setting AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY quality_rule_setting_keepers ON quality_rule_setting AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id)
                AND updated_by = current_actor_id());
CREATE POLICY quality_rule_setting_changers ON quality_rule_setting AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND updated_by = current_actor_id());
CREATE POLICY quality_rule_setting_removers ON quality_rule_setting AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));
CREATE POLICY project_quality_readers ON project_quality AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));

-- Whoever reads a requirement reads what the rules found in it. A revision's findings are written by whoever may
-- edit it, for its current revision, as the revision is made; the worker checks again as the admin role.
CREATE POLICY requirement_quality_readers ON requirement_quality AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY requirement_quality_writers ON requirement_quality AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id())
                AND revision = (SELECT r.revision FROM requirement r WHERE r.id = requirement_id));
CREATE POLICY quality_finding_readers ON quality_finding AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY quality_finding_writers ON quality_finding AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id())
                AND revision = (SELECT r.revision FROM requirement r WHERE r.id = requirement_id));

-- An editor accepts a finding of the current revision in their own name, or carries one forward unchanged from the
-- revision before; they take one back on the current revision alone.
CREATE POLICY quality_acceptance_readers ON quality_acceptance AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY quality_acceptance_givers ON quality_acceptance AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id())
                AND EXISTS (SELECT 1 FROM requirement r WHERE r.id = requirement_id AND r.revision = quality_acceptance.revision
                            AND project_open(r.project_id))
                AND ((carried_from IS NULL AND accepted_by = current_actor_id())
                     OR (carried_from IS NOT NULL AND EXISTS (
                         SELECT 1 FROM quality_acceptance p WHERE p.requirement_id = quality_acceptance.requirement_id
                         AND p.revision = quality_acceptance.revision - 1 AND p.key = quality_acceptance.key
                         AND p.reason = quality_acceptance.reason AND p.accepted_by IS NOT DISTINCT FROM quality_acceptance.accepted_by
                         AND COALESCE(p.carried_from, p.revision) = quality_acceptance.carried_from))));
CREATE POLICY quality_acceptance_takers ON quality_acceptance AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id())
           AND EXISTS (SELECT 1 FROM requirement r WHERE r.id = requirement_id AND r.revision = quality_acceptance.revision
                       AND project_open(r.project_id)));

GRANT SELECT, INSERT, UPDATE, DELETE ON quality_rule, quality_rule_setting, project_quality, requirement_quality,
    quality_finding, quality_acceptance TO exciter_admin;
REVOKE ALL ON quality_rule, quality_rule_setting, project_quality, requirement_quality, quality_finding,
    quality_acceptance FROM exciter_app;
GRANT SELECT ON quality_rule TO exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON quality_rule_setting TO exciter_app;
GRANT SELECT ON project_quality TO exciter_app;
GRANT SELECT, INSERT ON requirement_quality, quality_finding TO exciter_app;
GRANT SELECT, INSERT, DELETE ON quality_acceptance TO exciter_app;

-- Every project's requirements are checked once, by the worker, as a word list's change would have them.
INSERT INTO outbox_event (topic, payload) SELECT 'quality.recheck', jsonb_build_object('projectId', id) FROM project;

-- +goose Down
DELETE FROM outbox_event WHERE topic = 'quality.recheck';
DROP TABLE quality_acceptance, quality_finding, requirement_quality, project_quality, quality_rule_setting, quality_rule;
DROP FUNCTION project_quality_scores(uuid), requirement_quality_of(uuid), quality_rule_state(uuid, uuid), quality_acceptance_of_a_finding(),
    quality_weight(text), quality_words_fit(text[]);
ALTER TABLE project DROP CONSTRAINT project_language;
