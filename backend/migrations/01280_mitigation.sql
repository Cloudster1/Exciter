-- +goose Up
-- Mitigations (#260), as docs/api-contract-m13.md agrees: a link type may count as mitigation, a requirement that is
-- no risk mitigates a risk through it, and a mitigation is in effect once its requirement is implemented and
-- verified, a level its coverage rules do not ask counting as met.

ALTER TABLE link_type DROP CONSTRAINT link_type_counts_check;
ALTER TABLE link_type ADD CONSTRAINT link_type_counts_check CHECK (counts IN ('decomposition', 'verification', 'mitigation', 'nothing'));

-- +goose StatementBegin
-- The link types every installation starts with; mitigates goes suspect when the mitigating requirement changes,
-- so re-rating a risk leaves its mitigations as they are.
CREATE OR REPLACE FUNCTION link_type_seed() RETURNS void
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
    INSERT INTO link_type (key, name, forward, backward, acyclic, suspect_end, counts) VALUES
        ('derives', 'Derives', 'derives from', 'is derived into', true, 'target', 'decomposition'),
        ('refines', 'Refines', 'refines', 'is refined by', true, 'target', 'decomposition'),
        ('satisfies', 'Satisfies', 'satisfies', 'is satisfied by', true, 'target', 'decomposition'),
        ('verifies', 'Verifies', 'verifies', 'is verified by', true, 'target', 'verification'),
        ('conflicts', 'Conflicts', 'conflicts with', 'conflicts with', false, 'both', 'nothing'),
        ('relates', 'Relates', 'relates to', 'relates to', false, 'none', 'nothing'),
        ('mitigates', 'Mitigates', 'mitigates', 'is mitigated by', true, 'source', 'mitigation'),
        ('leads_to', 'Leads to', 'leads to', 'follows from', true, 'none', 'nothing')
$$;
-- +goose StatementEnd

-- An installation running already gains the two, unless one of its link types has the key or the name.
INSERT INTO link_type (key, name, forward, backward, acyclic, suspect_end, counts)
SELECT n.key, n.name, n.forward, n.backward, true, n.suspect_end, n.counts
FROM (VALUES ('mitigates', 'Mitigates', 'mitigates', 'is mitigated by', 'source', 'mitigation'),
             ('leads_to', 'Leads to', 'leads to', 'follows from', 'none', 'nothing')) n(key, name, forward, backward, suspect_end, counts)
WHERE EXISTS (SELECT 1 FROM link_type WHERE key = 'derives' AND project_id IS NULL)
  AND NOT EXISTS (SELECT 1 FROM link_type lt WHERE lt.key = n.key OR lower(lt.name) = lower(n.name));

-- +goose StatementBegin
-- Whether a requirement is a risk: a requirement, not a section, of a type marked as risks.
CREATE FUNCTION requirement_is_risk(target uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM requirement r JOIN requirement_type t ON t.id = r.type_id
                   WHERE r.id = target AND r.kind = 'requirement' AND t.role = 'risk')
$$;

-- A link of a type that counts as mitigation joins a requirement that is no risk to a risk.
CREATE FUNCTION trace_link_mitigates_risk() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM link_type lt WHERE lt.id = NEW.type_id AND lt.counts = 'mitigation')
       AND (NOT requirement_is_risk(NEW.target_id) OR requirement_is_risk(NEW.source_id)) THEN
        RAISE EXCEPTION 'a mitigation joins a requirement that is no risk to a risk'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_mitigates_risk';
    END IF;
    RETURN NEW;
END;
$$;

-- A link type starts counting as mitigation only while none of its links joins anything else.
CREATE FUNCTION link_type_counts_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.counts = 'mitigation' AND OLD.counts <> 'mitigation' AND EXISTS (
        SELECT 1 FROM trace_link l
        WHERE l.type_id = NEW.id AND l.deleted_at IS NULL
          AND (NOT requirement_is_risk(l.target_id) OR requirement_is_risk(l.source_id))) THEN
        RAISE EXCEPTION 'links of the type join what is no risk'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_counts_held';
    END IF;
    RETURN NEW;
END;
$$;

-- The mitigations of a risk the reader sees: each link that counts as mitigation into it, with its source's
-- coverage by the source's own project's rules, whether it is suspect, and whether it is in effect.
CREATE FUNCTION risk_mitigations(target_risk uuid)
    RETURNS TABLE (link_id uuid, source_id uuid, implemented text, verified text, suspect boolean, in_effect boolean)
    LANGUAGE sql STABLE ROWS 10
AS $$
    SELECT l.id, l.source_id, c.implemented, c.verified, COALESCE(st.suspect, false),
           s.trashed_at IS NULL AND NOT COALESCE(st.suspect, false)
           AND c.implemented IN ('full', 'na') AND c.verified IN ('full', 'na')
    FROM trace_link l
    JOIN link_type lt ON lt.id = l.type_id AND lt.counts = 'mitigation'
    JOIN requirement s ON s.id = l.source_id
    LEFT JOIN trace_link_state st ON st.id = l.id
    CROSS JOIN LATERAL (SELECT requirement_coverage(l.source_id, 'implemented') AS implemented,
                               requirement_coverage(l.source_id, 'verified') AS verified) c
    WHERE l.target_id = target_risk AND l.deleted_at IS NULL
$$;

-- The mitigations of many risks at once, whoever reads them, so a risk's state is the same for every reader; a
-- source the reader may not view is named by nothing.
CREATE FUNCTION risk_mitigation_rows(targets uuid[])
    RETURNS TABLE (risk_id uuid, link_id uuid, source_id uuid, source_project text, source_number integer, source_title text,
                   visible boolean, implemented text, verified text, suspect boolean, in_effect boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp ROWS 100
AS $$
    SELECT k.id, m.link_id, m.source_id, sp.key, s.number, s.title, perm_project_holds(s.project_id, current_actor_id(), 'view'),
           m.implemented, m.verified, m.suspect, m.in_effect
    FROM unnest(targets) k(id)
    CROSS JOIN LATERAL risk_mitigations(k.id) m
    JOIN requirement s ON s.id = m.source_id
    JOIN project sp ON sp.id = s.project_id
    ORDER BY k.id, sp.key, s.number
$$;

-- How far a risk is mitigated, whoever reads it: unmitigated while nothing mitigates it, claimed while a mitigation
-- is not in effect, mitigated once all are; null for what is no risk.
CREATE FUNCTION risk_mitigation_state(target_risk uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN NOT requirement_is_risk(target_risk) THEN NULL
                WHEN count(m.link_id) = 0 THEN 'unmitigated'
                WHEN bool_and(m.in_effect) THEN 'mitigated'
                ELSE 'claimed' END
    FROM risk_mitigations(target_risk) m
$$;

-- The side of a risk's rating in effect: after once it is mitigated and that side gives a level, before otherwise.
CREATE FUNCTION risk_effective(target_project uuid, rating jsonb, target_risk uuid) RETURNS jsonb
    LANGUAGE sql STABLE
AS $$
    SELECT CASE WHEN risk_mitigation_state(target_risk) = 'mitigated' AND risk_level_of(target_project, rating->'after') IS NOT NULL
                THEN rating->'after' ELSE rating->'before' END
$$;

-- A rating as it is read, now with the risk's state and the level of the side in effect.
CREATE OR REPLACE FUNCTION risk_view(target_project uuid, rating jsonb, target uuid) RETURNS jsonb
    LANGUAGE sql STABLE
AS $$
    SELECT jsonb_build_object(
        'beforeRpn', risk_rpn(rating->'before', s.detection),
        'afterRpn', risk_rpn(rating->'after', s.detection),
        'beforeLevel', risk_level_ref(risk_level_of(target_project, rating->'before')),
        'afterLevel', risk_level_ref(risk_level_of(target_project, rating->'after')),
        'state', CASE WHEN target IS NOT NULL THEN risk_mitigation_state(target) END,
        'level', CASE WHEN target IS NOT NULL THEN risk_level_ref(risk_level_of(target_project, risk_effective(target_project, rating, target))) END)
    FROM (SELECT (SELECT sc.detection FROM risk_scheme sc WHERE sc.project_id = target_project) AS detection) s
$$;
-- +goose StatementEnd

CREATE TRIGGER trace_link_mitigates_risk BEFORE INSERT OR UPDATE OF type_id, source_id, target_id ON trace_link
    FOR EACH ROW EXECUTE FUNCTION trace_link_mitigates_risk();
CREATE TRIGGER link_type_counts_held BEFORE UPDATE OF counts ON link_type
    FOR EACH ROW EXECUTE FUNCTION link_type_counts_held();

-- A type of risks keeps its role while a mitigation points at one of its risks, as while one is rated.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_type_role_holds(target_type uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.type_id = target_type
          AND (requirement_holds_test(r.preconditions, r.steps, r.parameters) OR r.risk IS NOT NULL
               OR EXISTS (SELECT 1 FROM trace_link l JOIN link_type lt ON lt.id = l.type_id AND lt.counts = 'mitigation'
                          WHERE l.target_id = r.id AND l.deleted_at IS NULL)))
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION requirement_is_risk(uuid), risk_mitigation_state(uuid), risk_mitigation_rows(uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION requirement_is_risk(uuid), risk_mitigation_state(uuid), risk_mitigation_rows(uuid[]) TO exciter_app, exciter_admin;

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_type_role_holds(target_type uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.type_id = target_type
          AND (requirement_holds_test(r.preconditions, r.steps, r.parameters) OR r.risk IS NOT NULL))
$$;

CREATE OR REPLACE FUNCTION risk_view(target_project uuid, rating jsonb, target uuid) RETURNS jsonb
    LANGUAGE sql STABLE
AS $$
    SELECT jsonb_build_object(
        'beforeRpn', risk_rpn(rating->'before', s.detection),
        'afterRpn', risk_rpn(rating->'after', s.detection),
        'beforeLevel', risk_level_ref(risk_level_of(target_project, rating->'before')),
        'afterLevel', risk_level_ref(risk_level_of(target_project, rating->'after')),
        'state', CASE WHEN target IS NOT NULL THEN 'unmitigated' END,
        'level', CASE WHEN target IS NOT NULL THEN risk_level_ref(risk_level_of(target_project, rating->'before')) END)
    FROM (SELECT (SELECT sc.detection FROM risk_scheme sc WHERE sc.project_id = target_project) AS detection) s
$$;

CREATE OR REPLACE FUNCTION link_type_seed() RETURNS void
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
    INSERT INTO link_type (key, name, forward, backward, acyclic, suspect_end, counts) VALUES
        ('derives', 'Derives', 'derives from', 'is derived into', true, 'target', 'decomposition'),
        ('refines', 'Refines', 'refines', 'is refined by', true, 'target', 'decomposition'),
        ('satisfies', 'Satisfies', 'satisfies', 'is satisfied by', true, 'target', 'decomposition'),
        ('verifies', 'Verifies', 'verifies', 'is verified by', true, 'target', 'verification'),
        ('conflicts', 'Conflicts', 'conflicts with', 'conflicts with', false, 'both', 'nothing'),
        ('relates', 'Relates', 'relates to', 'relates to', false, 'none', 'nothing')
$$;
-- +goose StatementEnd
DROP TRIGGER link_type_counts_held ON link_type;
DROP TRIGGER trace_link_mitigates_risk ON trace_link;
DROP FUNCTION risk_effective(uuid, jsonb, uuid);
DROP FUNCTION risk_mitigation_state(uuid);
DROP FUNCTION risk_mitigation_rows(uuid[]);
DROP FUNCTION risk_mitigations(uuid);
DROP FUNCTION link_type_counts_held();
DROP FUNCTION trace_link_mitigates_risk();
DROP FUNCTION requirement_is_risk(uuid);
-- The two link types this seeded go again while no link ever had them.
DELETE FROM link_type lt WHERE lt.project_id IS NULL AND lt.key IN ('mitigates', 'leads_to')
  AND NOT EXISTS (SELECT 1 FROM trace_link l WHERE l.type_id = lt.id);
UPDATE link_type SET counts = 'nothing' WHERE counts = 'mitigation';
ALTER TABLE link_type DROP CONSTRAINT link_type_counts_check;
ALTER TABLE link_type ADD CONSTRAINT link_type_counts_check CHECK (counts IN ('decomposition', 'verification', 'nothing'));
