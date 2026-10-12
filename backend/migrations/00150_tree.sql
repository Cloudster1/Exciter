-- +goose Up
-- The tree (#15): every requirement and section sits below another of its project, or at the top, ordered
-- among its siblings by rank, Stator's lexicographic positions. Outline numbers are computed on read. A move to
-- another project gives a requirement the next number there, and its old key stays behind and keeps resolving.
ALTER TABLE requirement
    ADD COLUMN parent_id   uuid,
    -- Byte order, whatever the database's collation: rank strings compare as base-62 fractions.
    ADD COLUMN rank        text COLLATE "C",
    ADD COLUMN copied_from uuid,
    -- A requirement whose parent is purged, having gone to the trash on its own before, moves to the top.
    ADD CONSTRAINT requirement_parent_fkey FOREIGN KEY (org_id, parent_id) REFERENCES requirement (org_id, id) ON DELETE SET NULL (parent_id),
    ADD CONSTRAINT requirement_copied_from_fkey FOREIGN KEY (org_id, copied_from) REFERENCES requirement (org_id, id) ON DELETE SET NULL (copied_from),
    ADD CONSTRAINT requirement_rank_shape CHECK (rank ~ '^[0-9A-Za-z]+$');

-- What was written before the tree goes to the top in the order of its numbers, as rank.Sequential writes them.
UPDATE requirement SET rank = lpad(to_hex(number), 10, '0') || 'V';
ALTER TABLE requirement ALTER COLUMN rank SET NOT NULL;

CREATE INDEX requirement_tree_idx ON requirement (org_id, project_id, parent_id, rank);

-- A number, and a rank where an insert names none, which puts it after what came before at the top.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_number() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    UPDATE project SET next_number = next_number + 1
    WHERE org_id = NEW.org_id AND id = NEW.project_id
    RETURNING next_number - 1 INTO NEW.number;
    IF NEW.number IS NULL THEN
        RAISE EXCEPTION 'a requirement needs a project of its own organization'
            USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'requirement_project_fkey';
    END IF;
    IF NEW.rank IS NULL THEN
        NEW.rank := lpad(to_hex(NEW.number), 10, '0') || 'V';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A requirement may sit below anything of its own project except itself or what is below it, which would cut
-- it off from the top. Two moves in one project that are each fine alone can close a loop together, so a
-- change of parent takes the project's tree lock first; a move between two projects takes both, in one order.
-- +goose StatementBegin
CREATE FUNCTION requirement_tree_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('requirement-tree:' || least(OLD.project_id, NEW.project_id)::text, 0));
        PERFORM pg_advisory_xact_lock(hashtextextended('requirement-tree:' || greatest(OLD.project_id, NEW.project_id)::text, 0));
    ELSE
        PERFORM pg_advisory_xact_lock(hashtextextended('requirement-tree:' || NEW.project_id::text, 0));
    END IF;
    IF NEW.parent_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM requirement WHERE org_id = NEW.org_id AND id = NEW.parent_id AND project_id = NEW.project_id) THEN
        RAISE EXCEPTION 'a requirement sits below one of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_parent_same_project';
    END IF;
    IF NEW.parent_id = NEW.id OR EXISTS (
        WITH RECURSIVE above (id, parent_id) AS (
            SELECT id, parent_id FROM requirement WHERE org_id = NEW.org_id AND id = NEW.parent_id
            UNION
            SELECT r.id, r.parent_id FROM requirement r JOIN above a ON r.org_id = NEW.org_id AND r.id = a.parent_id
        )
        SELECT 1 FROM above WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'a requirement cannot go below itself or below one of the requirements under it'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_tree_no_cycle';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_tree_check BEFORE INSERT OR UPDATE OF parent_id, project_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_tree_guard();

-- The number and the kind never change; the project changes only by a move, which takes the next number of
-- the project it goes to and leaves its old key behind, whoever moves it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.number <> OLD.number OR NEW.kind <> OLD.kind THEN
        RAISE EXCEPTION 'a requirement keeps its number and its kind'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER requirement_fixed ON requirement;
CREATE TRIGGER requirement_fixed BEFORE UPDATE OF number, kind ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_fixed();

-- +goose StatementBegin
CREATE FUNCTION requirement_rehome() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    old_key text;
BEGIN
    IF NEW.project_id = OLD.project_id THEN
        RETURN NEW;
    END IF;
    SELECT key || '-R' || OLD.number INTO old_key FROM project WHERE org_id = OLD.org_id AND id = OLD.project_id;
    UPDATE project SET next_number = next_number + 1
    WHERE org_id = NEW.org_id AND id = NEW.project_id
    RETURNING next_number - 1 INTO NEW.number;
    IF NEW.number IS NULL THEN
        RAISE EXCEPTION 'a requirement moves to a project of its own organization'
            USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'requirement_project_fkey';
    END IF;
    INSERT INTO requirement_old_key (org_id, key, requirement_id) VALUES (OLD.org_id, old_key, OLD.id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Named to run after requirement_fixed, which still sees the number the row came with.
CREATE TRIGGER requirement_rehome BEFORE UPDATE OF project_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_rehome();

-- A move to another project carries the type and the values along, which must be the new project's.
DROP TRIGGER requirement_attributes_check ON requirement;
CREATE TRIGGER requirement_attributes_check BEFORE INSERT OR UPDATE OF attributes, type_id, project_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_attributes_check();

-- +goose Down
DROP TRIGGER IF EXISTS requirement_attributes_check ON requirement;
CREATE TRIGGER requirement_attributes_check BEFORE INSERT OR UPDATE OF attributes, type_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_attributes_check();
DROP TRIGGER IF EXISTS requirement_rehome ON requirement;
DROP FUNCTION IF EXISTS requirement_rehome();
DROP TRIGGER IF EXISTS requirement_tree_check ON requirement;
DROP FUNCTION IF EXISTS requirement_tree_guard();
DROP TRIGGER IF EXISTS requirement_fixed ON requirement;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.number <> OLD.number OR NEW.project_id <> OLD.project_id OR NEW.kind <> OLD.kind THEN
        RAISE EXCEPTION 'a requirement keeps its number, its project and its kind'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER requirement_fixed BEFORE UPDATE OF number, project_id, kind ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_fixed();
DROP INDEX IF EXISTS requirement_tree_idx;
ALTER TABLE requirement
    DROP CONSTRAINT IF EXISTS requirement_rank_shape,
    DROP CONSTRAINT IF EXISTS requirement_copied_from_fkey,
    DROP CONSTRAINT IF EXISTS requirement_parent_fkey,
    DROP COLUMN IF EXISTS copied_from,
    DROP COLUMN IF EXISTS rank,
    DROP COLUMN IF EXISTS parent_id;
