-- +goose Up
-- Word import (#209), as docs/api-contract-m10.md agrees: a Word document comes in through the ReqIF import, which
-- keeps the document as read and builds what it becomes from the choices each time. An import may go below a
-- section of its project, and a Word document's parts count with the choices, until it starts.

ALTER TABLE reqif_import DROP CONSTRAINT reqif_import_format_check,
    ADD CONSTRAINT reqif_import_format_check CHECK (format IN ('reqif', 'reqifz', 'docx')),
    ADD COLUMN parent_id uuid REFERENCES requirement (id) ON DELETE SET NULL;

-- +goose StatementBegin
-- The file of an import and where it goes never change, its choices and how many parts it makes only until it starts,
-- and its state only moves forward.
CREATE OR REPLACE FUNCTION reqif_import_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.created_by, NEW.file_name, NEW.format, NEW.content, NEW.created_at)
       IS DISTINCT FROM (OLD.project_id, OLD.created_by, OLD.file_name, OLD.format, OLD.content, OLD.created_at)
       OR (NEW.parent_id IS DISTINCT FROM OLD.parent_id AND NEW.parent_id IS NOT NULL) THEN
        RAISE EXCEPTION 'reqif_import_file_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The file of an import never changes; upload it again instead.';
    END IF;
    IF OLD.state <> 'uploaded' AND (NEW.choices, NEW.total) IS DISTINCT FROM (OLD.choices, OLD.total) THEN
        RAISE EXCEPTION 'reqif_import_choices_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The choices of an import that started never change.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'uploaded' AND NEW.state IN ('queued', 'cancelled'))
        OR (OLD.state = 'queued' AND NEW.state IN ('running', 'cancelled'))
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed', 'cancelled'))) THEN
        RAISE EXCEPTION 'reqif_import_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'An import moves from uploaded to queued to running, and then ends.';
    END IF;
    RETURN NEW;
END;
$$;

-- An import goes below a section of its own project.
CREATE FUNCTION reqif_import_parent() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM requirement r WHERE r.id = NEW.parent_id AND r.project_id = NEW.project_id AND r.kind = 'section'
        AND r.trashed_at IS NULL) THEN
        RAISE EXCEPTION 'reqif_import_parent' USING ERRCODE = 'check_violation', CONSTRAINT = 'reqif_import_parent',
            DETAIL = 'An import goes below a section of its own project.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER reqif_import_parent BEFORE INSERT ON reqif_import FOR EACH ROW EXECUTE FUNCTION reqif_import_parent();

-- +goose Down
DROP TRIGGER reqif_import_parent ON reqif_import;
DROP FUNCTION reqif_import_parent();
DELETE FROM reqif_import WHERE format = 'docx';
ALTER TABLE reqif_import DROP COLUMN parent_id,
    DROP CONSTRAINT reqif_import_format_check,
    ADD CONSTRAINT reqif_import_format_check CHECK (format IN ('reqif', 'reqifz'));
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reqif_import_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.created_by, NEW.file_name, NEW.format, NEW.content, NEW.total, NEW.created_at)
       IS DISTINCT FROM (OLD.project_id, OLD.created_by, OLD.file_name, OLD.format, OLD.content, OLD.total, OLD.created_at) THEN
        RAISE EXCEPTION 'reqif_import_file_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The file of an import never changes; upload it again instead.';
    END IF;
    IF OLD.state <> 'uploaded' AND NEW.choices IS DISTINCT FROM OLD.choices THEN
        RAISE EXCEPTION 'reqif_import_choices_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The choices of an import that started never change.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'uploaded' AND NEW.state IN ('queued', 'cancelled'))
        OR (OLD.state = 'queued' AND NEW.state IN ('running', 'cancelled'))
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed', 'cancelled'))) THEN
        RAISE EXCEPTION 'reqif_import_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'An import moves from uploaded to queued to running, and then ends.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
