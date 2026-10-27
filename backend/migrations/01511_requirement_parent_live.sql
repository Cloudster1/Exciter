-- +goose Up
-- A requirement out of the trash goes below one out of the trash only (#310). The service's place refused a parent in
-- the trash already, but a write straight to the table put a live requirement where neither the tree nor the trash
-- shows it. Only a new parent is checked, so what a section takes to the trash with it and brings back stays put.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_tree_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
    IF NOT EXISTS (SELECT 1 FROM requirement WHERE id = NEW.parent_id AND project_id = NEW.project_id) THEN
        RAISE EXCEPTION 'a requirement sits below one of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_parent_same_project';
    END IF;
    IF NEW.trashed_at IS NULL AND (TG_OP = 'INSERT' OR NEW.parent_id IS DISTINCT FROM OLD.parent_id)
       AND EXISTS (SELECT 1 FROM requirement WHERE id = NEW.parent_id AND trashed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'a requirement out of the trash sits below one out of the trash'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_parent_live';
    END IF;
    IF NEW.parent_id = NEW.id OR EXISTS (
        WITH RECURSIVE above (id, parent_id) AS (
            SELECT id, parent_id FROM requirement WHERE id = NEW.parent_id
            UNION
            SELECT r.id, r.parent_id FROM requirement r JOIN above a ON r.id = a.parent_id
        )
        SELECT 1 FROM above WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'a requirement cannot go below itself or below one of the requirements under it'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_tree_no_cycle';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_tree_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
    IF NOT EXISTS (SELECT 1 FROM requirement WHERE id = NEW.parent_id AND project_id = NEW.project_id) THEN
        RAISE EXCEPTION 'a requirement sits below one of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_parent_same_project';
    END IF;
    IF NEW.parent_id = NEW.id OR EXISTS (
        WITH RECURSIVE above (id, parent_id) AS (
            SELECT id, parent_id FROM requirement WHERE id = NEW.parent_id
            UNION
            SELECT r.id, r.parent_id FROM requirement r JOIN above a ON r.id = a.parent_id
        )
        SELECT 1 FROM above WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'a requirement cannot go below itself or below one of the requirements under it'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_tree_no_cycle';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd
