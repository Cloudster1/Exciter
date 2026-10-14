-- +goose Up
-- The functions and triggers keep their project, owner and creator rules without asking for the organization
-- (#106): an installation holds one, and the keys of 00411 no longer carry it. Rows they write take org_id from
-- its default (00412). Only current_org_id, the organization's own link type seed and the session's own column
-- stay until #108. Functions that took the organization as an argument are made again without it.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.administered_without(leaving uuid)
 RETURNS boolean
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM org_member m JOIN app_user u ON u.id = m.user_id
                   WHERE m.user_id <> leaving AND m.org_role IN ('owner', 'admin') AND u.is_active);
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION administered_without(uuid) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.api_token_fixed()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF (NEW.user_id, NEW.name, NEW.token_hash, NEW.scopes, NEW.projects_only, NEW.expires_at, NEW.created_at)
       IS DISTINCT FROM (OLD.user_id, OLD.name, OLD.token_hash, OLD.scopes, OLD.projects_only, OLD.expires_at, OLD.created_at) THEN
        RAISE EXCEPTION 'api_token_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A token stays what it was made; make a new token instead.';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.api_token_project_addable(token uuid, project uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM api_token t
                   WHERE t.id = token AND t.projects_only
                     AND t.user_id = current_actor_id() AND t.created_at = now())
       AND perm_project_holds(project, current_actor_id(), 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.app_user_keeps_an_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active AND EXISTS (
        SELECT 1 FROM org_member m
        WHERE m.user_id = NEW.id AND m.org_role IN ('owner', 'admin')
          AND NOT administered_without(NEW.id)) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connected_count()
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN current_actor_administers()
                THEN (SELECT count(*)::integer FROM armature_token)
           END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connection_forgets_links()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_remote_link;
    END IF;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connection_moved()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_token;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_endpoint()
 RETURNS TABLE(base_url text, armature_org_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.base_url, c.armature_org_id
    FROM armature_connection c
    WHERE EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = current_actor_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_issue_link_open()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM armature_connection) THEN
        RAISE EXCEPTION 'Exciter is not connected to Armature' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM requirement WHERE id = NEW.requirement_id
                   AND kind = 'requirement' AND trashed_at IS NULL) THEN
        RAISE EXCEPTION 'only a requirement out of the trash takes Armature issues' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit(target uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF actor IS NULL OR NOT EXISTS (SELECT 1 FROM armature_connection) THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM armature_issue_link WHERE requirement_id = target)
       AND NOT EXISTS (SELECT 1 FROM armature_remote_link WHERE requirement_id = target) THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM outbox_event
               WHERE topic = 'armature.links' AND processed_at IS NULL
                 AND xmin = pg_current_xact_id()::xid AND payload ->> 'requirementId' = target::text) THEN
        RETURN;
    END IF;
    INSERT INTO outbox_event (topic, payload)
    VALUES ('armature.links', jsonb_build_object('requirementId', target, 'actorId', actor));
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_project_rekeyed()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM armature_links_emit(l.requirement_id)
    FROM (SELECT DISTINCT a.requirement_id FROM armature_issue_link a
          JOIN requirement r ON r.id = a.requirement_id
          WHERE r.project_id = NEW.id) l;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_kept()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement r WHERE r.id = OLD.requirement_id)
       AND attachment_referenced(OLD.requirement_id, OLD.id) THEN
        RAISE EXCEPTION 'attachment_referenced' USING ERRCODE = 'check_violation',
            DETAIL = 'A revision of the requirement shows this file, so it is taken off the list instead.';
    END IF;
    RETURN OLD;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_leave_tombstone()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    INSERT INTO attachment_tombstone (object_key) VALUES (OLD.object_key)
        ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_referenced(requirement uuid, attachment uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM requirement_revision rv
        WHERE rv.requirement_id = attachment_referenced.requirement
          AND jsonb_path_exists(jsonb_build_array(rv.statement, rv.rationale, rv.attributes),
                'lax $.** ? ((@.type == "image" || @.type == "attachment") && @.attrs.attachmentId == $id)',
                jsonb_build_object('id', attachment_referenced.attachment::text)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attribute_project(attribute uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT project_id FROM attribute_def WHERE id = attribute
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attribute_usage(attribute uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT (SELECT count(*) FROM requirement WHERE attributes ? attribute_usage.attribute::text)::int
         + (SELECT count(*) FROM requirement_revision WHERE attributes ? attribute_usage.attribute::text)::int
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.audit_log_prune(cutoff timestamp with time zone)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF cutoff > now() - interval '1 day' THEN
        RAISE EXCEPTION 'the audit log keeps every entry for at least a day' USING ERRCODE = '22023';
    END IF;
    DELETE FROM audit_log WHERE created_at < cutoff;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.current_actor_administers()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_whole(current_actor_id()) AND EXISTS (
        SELECT 1 FROM org_member
        WHERE user_id = current_actor_id() AND org_role IN ('owner', 'admin'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.group_member_scope_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member here can join a group' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.import_job_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF (NEW.project_id, NEW.created_by, NEW.file_name, NEW.headers, NEW.rows, NEW.total, NEW.created_at)
       IS DISTINCT FROM (OLD.project_id, OLD.created_by, OLD.file_name, OLD.headers, OLD.rows, OLD.total, OLD.created_at) THEN
        RAISE EXCEPTION 'import_job_file_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The file of an import never changes; upload it again instead.';
    END IF;
    IF OLD.state <> 'uploaded' AND (NEW.mapping, NEW.choices) IS DISTINCT FROM (OLD.mapping, OLD.choices) THEN
        RAISE EXCEPTION 'import_job_mapping_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The mapping of an import that started never changes.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'uploaded' AND NEW.state IN ('queued', 'cancelled'))
        OR (OLD.state = 'queued' AND NEW.state IN ('running', 'cancelled'))
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed', 'cancelled'))) THEN
        RAISE EXCEPTION 'import_job_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'An import moves from uploaded to queued to running, and then ends.';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_names_free()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.key <> OLD.key OR NEW.project_id IS DISTINCT FROM OLD.project_id) THEN
        RAISE EXCEPTION 'a link type keeps its key and its project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_key_fixed';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('link_type', 0));
    IF EXISTS (SELECT 1 FROM link_type t
               WHERE t.id <> NEW.id AND t.key = NEW.key
                 AND (NEW.project_id IS NULL) <> (t.project_id IS NULL)) THEN
        RAISE EXCEPTION 'the link type key % is taken', NEW.key
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'link_type_key_taken';
    END IF;
    IF EXISTS (SELECT 1 FROM link_type t
               WHERE t.id <> NEW.id AND lower(t.name) = lower(NEW.name)
                 AND (NEW.project_id IS NULL) <> (t.project_id IS NULL)) THEN
        RAISE EXCEPTION 'the link type name % is taken', NEW.name
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'link_type_name_taken';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_project_reach()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM link_type t WHERE t.id = NEW.link_type_id
                   AND (t.project_id IS NULL OR t.project_id = NEW.project_id)) THEN
        RAISE EXCEPTION 'a project sets the organization''s link types and its own'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_rule_reach()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement_type rt
               WHERE rt.id IN (NEW.source_type_id, NEW.target_type_id) AND rt.project_id <> NEW.project_id) THEN
        RAISE EXCEPTION 'a link type rule names requirement types of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_usage(type uuid, live_only boolean)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT count(*)::int FROM trace_link
    WHERE type_id = link_type_usage.type AND (NOT live_only OR deleted_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_default_theme_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND shared
    ) THEN
        RAISE EXCEPTION 'the organization''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_member_keeps_an_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.org_role IN ('owner', 'admin') AND (TG_OP = 'DELETE' OR NEW.org_role NOT IN ('owner', 'admin'))
       AND EXISTS (SELECT 1 FROM org)
       AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND is_active)
       AND NOT administered_without(OLD.user_id) THEN
        RAISE EXCEPTION 'the installation keeps its last active administrator' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_project_holds(project uuid, actor uuid, permission text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_reaches(actor, perm_project_holds.project) AND EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.user_id = actor
        WHERE p.id = perm_project_holds.project AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.org_role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.project_id = p.id
                AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.org_role, g.role))))))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_project_roles(target uuid, actor uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT role), '{}') FROM (
        SELECT 'admin' AS role FROM org_member m
        WHERE m.user_id = actor AND m.org_role IN ('owner', 'admin') AND EXISTS (SELECT 1 FROM project p WHERE p.id = target)
        UNION ALL
        SELECT perm_grant_role(m.org_role, g.role) FROM project_grant g
        JOIN org_member m ON m.user_id = actor
        WHERE g.project_id = target AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
    ) roles
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_requirement_editable(requirement uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = perm_requirement_editable.requirement AND perm_project_holds(r.project_id, actor, 'edit'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_requirement_viewable(requirement uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = perm_requirement_viewable.requirement AND perm_project_holds(r.project_id, actor, 'view')
          AND (r.trashed_at IS NULL OR perm_project_holds(r.project_id, actor, 'edit')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_grant_defaults()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.created_by AND org_role <> 'guest') THEN
        INSERT INTO project_grant (project_id, subject_type, user_id, role)
        VALUES (NEW.id, 'user', NEW.created_by, 'admin');
    END IF;
    INSERT INTO project_grant (project_id, subject_type, role) VALUES (NEW.id, 'everyone', 'editor');
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_grant_guest_check()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.role = 'admin' AND NEW.subject_type = 'user' AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot be an administrator of a project. Make them an editor, or a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_grant_guest_not_admin';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_alias_check()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM project_key_claim(NEW.key, NEW.project_id, true);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_check()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM project_key_claim(NEW.key, NEW.id, false);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_claim(claimed text, claimant uuid, as_alias boolean)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('project_key/' || claimed, 0));
    IF (as_alias AND EXISTS (SELECT 1 FROM project WHERE key = claimed))
       OR (NOT as_alias AND EXISTS (SELECT 1 FROM project_key_alias WHERE key = claimed AND project_id <> claimant)) THEN
        RAISE EXCEPTION 'the key % is taken by another project', claimed
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'project_key_taken';
    END IF;
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION project_key_claim(text, uuid, boolean) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_keep()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.key IS DISTINCT FROM OLD.key THEN
        DELETE FROM project_key_alias WHERE key = NEW.key AND project_id = NEW.id;
        INSERT INTO project_key_alias (key, project_id) VALUES (OLD.key, OLD.id);
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_taken(claimed text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM project WHERE key = claimed)
        OR EXISTS (SELECT 1 FROM project_key_alias WHERE key = claimed)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_open(project uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM project p WHERE p.id = project_open.project AND p.archived_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_type_default()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    made uuid;
BEGIN
    INSERT INTO requirement_type (project_id, name, icon, colour, position)
    VALUES (NEW.id, 'Requirement', 'requirement', '#4f6bed', 0) RETURNING id INTO made;
    UPDATE project SET settings = jsonb_set(settings, '{defaultTypeId}', to_jsonb(made::text)) WHERE id = NEW.id;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_workflow_default(target uuid)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    flow uuid;
    draft uuid; review uuid; approved uuid; implemented uuid; verified uuid; rejected uuid; obsolete uuid;
    editors jsonb := '{"roles": ["editor", "admin"]}';
    reviewers jsonb := '{"roles": ["reviewer", "admin"]}';
BEGIN
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'Draft', 'draft', 0) RETURNING id INTO draft;
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'In review', 'review', 1) RETURNING id INTO review;
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'Approved', 'approved', 2) RETURNING id INTO approved;
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'Implemented', 'done', 3) RETURNING id INTO implemented;
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'Verified', 'done', 4) RETURNING id INTO verified;
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'Rejected', 'closed', 5) RETURNING id INTO rejected;
    INSERT INTO req_status (project_id, name, category, position) VALUES (target, 'Obsolete', 'closed', 6) RETURNING id INTO obsolete;
    INSERT INTO workflow (project_id, name, description)
    VALUES (target, 'Default workflow', 'Draft, review, approval, implementation and verification.') RETURNING id INTO flow;
    INSERT INTO workflow_step (workflow_id, status_id, is_initial, on_revision_status_id, layout_x, layout_y) VALUES
        (flow, draft, true, NULL, 40, 120), (flow, review, false, NULL, 280, 120), (flow, approved, false, draft, 520, 120),
        (flow, implemented, false, draft, 760, 120), (flow, verified, false, draft, 1000, 120),
        (flow, rejected, false, NULL, 400, 320), (flow, obsolete, false, NULL, 640, 320);
    PERFORM workflow_default_edge(flow, 'Submit for review', draft, review, 0, editors, false, false);
    PERFORM workflow_default_edge(flow, 'Approve', review, approved, 1, reviewers, false, true);
    PERFORM workflow_default_edge(flow, 'Send back', review, draft, 2, reviewers, true, false);
    PERFORM workflow_default_edge(flow, 'Mark implemented', approved, implemented, 3, editors, false, false);
    PERFORM workflow_default_edge(flow, 'Mark verified', implemented, verified, 4, editors, false, false);
    PERFORM workflow_default_edge(flow, 'Reject', NULL, rejected, 5, reviewers, true, false);
    PERFORM workflow_default_edge(flow, 'Make obsolete', NULL, obsolete, 6, editors, false, false);
    PERFORM workflow_default_edge(flow, 'Reopen', rejected, draft, 7, editors, false, false);
    PERFORM workflow_default_edge(flow, 'Reopen', obsolete, draft, 8, editors, false, false);
    UPDATE project SET settings = jsonb_set(settings, '{workflowId}', to_jsonb(flow::text)) WHERE id = target;
    RETURN flow;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_attributes_check()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    project uuid;
    stray   text;
BEGIN
    IF TG_TABLE_NAME = 'requirement' THEN
        project := NEW.project_id;
    ELSE
        SELECT r.project_id INTO project FROM requirement r WHERE r.id = NEW.requirement_id;
    END IF;
    IF NEW.type_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM requirement_type t WHERE t.id = NEW.type_id AND t.project_id = project) THEN
        RAISE EXCEPTION 'a requirement takes a type of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_type_same_project';
    END IF;
    SELECT k INTO stray FROM jsonb_object_keys(NEW.attributes) AS k
    WHERE NOT EXISTS (SELECT 1 FROM attribute_def a WHERE a.project_id = project AND a.id::text = k)
    LIMIT 1;
    IF stray IS NOT NULL THEN
        RAISE EXCEPTION 'attribute % is not one of the requirement''s project', stray
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_attributes_same_project';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_import_status(requirement uuid, status uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_import_status.requirement FOR UPDATE;
    IF r.id IS NULL OR r.kind <> 'requirement' OR r.created_at <> now() OR r.revision <> 1
       OR r.created_by IS DISTINCT FROM current_actor_id()
       OR NOT perm_project_holds(r.project_id, current_actor_id(), 'administer') THEN
        RAISE EXCEPTION 'requirement_import_status' USING ERRCODE = 'insufficient_privilege',
            DETAIL = 'An import sets the status of a requirement it has just made, and of no other.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = requirement_workflow(r.type_id, r.project_id)
                   AND status_id = requirement_import_status.status) THEN
        RAISE EXCEPTION 'requirement_import_status_unknown' USING ERRCODE = 'check_violation',
            DETAIL = 'The status is not one of the requirement''s workflow.';
    END IF;
    UPDATE requirement SET status_id = requirement_import_status.status WHERE id = r.id;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_number()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    UPDATE project SET next_number = next_number + 1
    WHERE id = NEW.project_id
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_rehome()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    old_key text;
BEGIN
    IF NEW.project_id = OLD.project_id THEN
        RETURN NEW;
    END IF;
    SELECT key || '-R' || OLD.number INTO old_key FROM project WHERE id = OLD.project_id;
    UPDATE project SET next_number = next_number + 1
    WHERE id = NEW.project_id
    RETURNING next_number - 1 INTO NEW.number;
    IF NEW.number IS NULL THEN
        RAISE EXCEPTION 'a requirement moves to a project that exists'
            USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'requirement_project_fkey';
    END IF;
    INSERT INTO requirement_old_key (key, requirement_id) VALUES (old_key, OLD.id);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_remap_status(requirement uuid, status uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    flow uuid;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_remap_status.requirement FOR UPDATE;
    IF r.id IS NULL OR r.kind <> 'requirement' OR NOT perm_project_holds(r.project_id, current_actor_id(), 'edit') THEN
        RAISE EXCEPTION 'the requirement is not the actor''s to change'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_remap_allowed';
    END IF;
    flow := requirement_workflow(r.type_id, r.project_id);
    IF EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = flow AND status_id = r.status_id)
       OR NOT EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = flow AND status_id = requirement_remap_status.status) THEN
        RAISE EXCEPTION 'a status is remapped only from one the workflow lacks to one it has'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_remap_lacking';
    END IF;
    UPDATE requirement SET status_id = requirement_remap_status.status WHERE id = r.id;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_revision_next()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    current integer;
BEGIN
    SELECT revision INTO current FROM requirement WHERE id = NEW.requirement_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'revision % of a requirement is not the one after its current revision %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_revision_in_sequence';
    END IF;
    UPDATE requirement SET revision = NEW.number WHERE id = NEW.requirement_id;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_revision_status(requirement uuid)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    target uuid;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_revision_status.requirement FOR UPDATE;
    IF r.id IS NULL OR NOT perm_project_holds(r.project_id, current_actor_id(), 'edit') THEN
        RETURN NULL;
    END IF;
    SELECT on_revision_status_id INTO target FROM workflow_step
    WHERE workflow_id = requirement_workflow(r.type_id, r.project_id) AND status_id = r.status_id;
    IF target IS NULL OR target = r.status_id THEN
        RETURN NULL;
    END IF;
    UPDATE requirement SET status_id = target WHERE id = r.id;
    RETURN r.status_id;
END;
$function$;
-- +goose StatementEnd

-- Its rows no longer say which organization they are of, so its type is made again.

DROP FUNCTION requirement_search_rows(tsquery, text, integer);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_search_rows(q tsquery, key_project text, key_number integer)
 RETURNS TABLE(id uuid, project_id uuid, number integer, kind text, title text, statement jsonb, rationale jsonb, revision integer, status_id uuid, owner_id uuid, updated_at timestamp with time zone, updated_by uuid, rank real, key_match boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH keyed AS MATERIALIZED (
        SELECT r.id FROM requirement r JOIN project p ON p.id = r.project_id
        WHERE p.key = key_project AND r.number = key_number
    ), hit AS MATERIALIZED (
        SELECT r.id, r.project_id, r.number, r.kind, r.title, r.statement, r.rationale, r.revision, r.status_id, r.owner_id,
               r.updated_at, r.updated_by, ts_rank(r.search_vector, q) AS rank, r.id IN (SELECT k.id FROM keyed k) AS key_match
        FROM requirement r
        WHERE r.trashed_at IS NULL
          AND (r.search_vector @@ q OR r.id IN (SELECT k.id FROM keyed k))
    ), judged AS MATERIALIZED (
        SELECT j.project_id, perm_project_holds(j.project_id, current_actor_id(), 'view') AS holds
        FROM (SELECT DISTINCT h.project_id FROM hit h) j
    )
    SELECT h.id, h.project_id, h.number, h.kind, h.title, h.statement, h.rationale, h.revision,
           h.status_id, h.owner_id, h.updated_at, h.updated_by, h.rank, h.key_match
    FROM hit h JOIN judged j ON j.project_id = h.project_id
    WHERE j.holds
$function$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION requirement_search_rows(tsquery, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION requirement_search_rows(tsquery, text, integer) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_transition(requirement uuid, transition uuid, note text DEFAULT ''::text)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    t workflow_transition%ROWTYPE;
    actor uuid := current_actor_id();
    new_owner uuid;
    post text;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_transition.requirement FOR UPDATE;
    SELECT * INTO t FROM workflow_transition WHERE id = requirement_transition.transition;
    IF r.id IS NULL OR t.id IS NULL OR r.trashed_at IS NOT NULL OR t.workflow_id <> requirement_workflow(r.type_id, r.project_id) THEN
        RAISE EXCEPTION 'that transition is not one of the requirement''s workflow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_known';
    END IF;
    IF NOT (perm_project_holds(r.project_id, actor, 'edit') OR (perm_project_holds(r.project_id, actor, 'approve') AND EXISTS (
        SELECT 1 FROM transition_rule x WHERE x.transition_id = t.id AND x.type = 'condition.project_role' AND x.config -> 'roles' ? 'reviewer'))) THEN
        RAISE EXCEPTION 'the transition is not the actor''s to take'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_transition_allowed';
    END IF;
    IF (t.from_status_id IS NOT NULL AND t.from_status_id IS DISTINCT FROM r.status_id)
       OR (t.from_status_id IS NULL AND t.to_status_id = r.status_id) THEN
        RAISE EXCEPTION 'the transition does not leave the requirement''s status'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_leaves';
    END IF;
    new_owner := r.owner_id;
    FOR post IN SELECT x.type FROM transition_rule x WHERE x.transition_id = t.id AND x.kind = 'postfunction' ORDER BY x.position LOOP
        new_owner := CASE post WHEN 'postfunction.assign_to_actor' THEN actor WHEN 'postfunction.clear_owner' THEN NULL ELSE new_owner END;
    END LOOP;
    UPDATE requirement SET status_id = t.to_status_id, owner_id = new_owner, updated_by = actor WHERE id = r.id;
    INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (r.id, 'status', COALESCE((SELECT name FROM req_status WHERE id = r.status_id), ''),
            (SELECT name FROM req_status WHERE id = t.to_status_id), COALESCE(note, ''), actor);
    IF new_owner IS DISTINCT FROM r.owner_id THEN
        INSERT INTO requirement_change (requirement_id, field, from_value, to_value, actor_id)
        VALUES (r.id, 'owner', COALESCE((SELECT name FROM app_user WHERE id = r.owner_id), ''),
                COALESCE((SELECT name FROM app_user WHERE id = new_owner), ''), actor);
    END IF;
    RETURN t.to_status_id;
END;
$function$;
-- +goose StatementEnd

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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.theme_in_use(theme uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT count(*)::integer FROM user_theme ut
    WHERE ut.theme_id = theme_in_use.theme
      AND EXISTS (SELECT 1 FROM theme t WHERE t.id = ut.theme_id
                  AND (t.owner_id = current_actor_id() OR t.shared))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.theme_unshared_default()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.trace_link_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    t        link_type;
    src      requirement;
    tgt      requirement;
    way_back boolean;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('trace_link', 0));
    SELECT * INTO t FROM link_type WHERE id = NEW.type_id;
    SELECT * INTO src FROM requirement WHERE id = NEW.source_id;
    SELECT * INTO tgt FROM requirement WHERE id = NEW.target_id;
    IF src.kind <> 'requirement' OR tgt.kind <> 'requirement' OR src.trashed_at IS NOT NULL OR tgt.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a link joins two requirements out of the trash'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_ends';
    END IF;
    IF t.retired OR (t.project_id IS NOT NULL AND t.project_id <> src.project_id) THEN
        RAISE EXCEPTION 'a link takes a live type of its source''s project or one for all projects'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_type';
    END IF;
    -- A type whose phrases read the same both ways is symmetric, so B to A repeats A to B.
    IF EXISTS (SELECT 1 FROM trace_link l
               WHERE l.type_id = NEW.type_id AND l.deleted_at IS NULL
                 AND ((l.source_id = NEW.source_id AND l.target_id = NEW.target_id)
                      OR (t.forward = t.backward AND l.source_id = NEW.target_id AND l.target_id = NEW.source_id))) THEN
        RAISE EXCEPTION 'those requirements are already linked that way'
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'trace_link_repeat';
    END IF;
    IF (SELECT count(*) FROM trace_link l WHERE l.deleted_at IS NULL
          AND (l.source_id = NEW.source_id OR l.target_id = NEW.source_id)) >= 500
       OR (SELECT count(*) FROM trace_link l WHERE l.deleted_at IS NULL
          AND (l.source_id = NEW.target_id OR l.target_id = NEW.target_id)) >= 500 THEN
        RAISE EXCEPTION 'a requirement holds at most 500 links'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_limit';
    END IF;
    IF t.acyclic THEN
        WITH RECURSIVE downstream AS (
            SELECT l.target_id, ARRAY[l.target_id] AS path
            FROM trace_link l
            WHERE l.type_id = NEW.type_id AND l.deleted_at IS NULL AND l.source_id = NEW.target_id
            UNION ALL
            SELECT l.target_id, d.path || l.target_id
            FROM trace_link l
            JOIN downstream d ON l.source_id = d.target_id
            WHERE l.type_id = NEW.type_id AND l.deleted_at IS NULL
              AND array_length(d.path, 1) < 64 AND NOT l.target_id = ANY (d.path)
        )
        SELECT EXISTS (SELECT 1 FROM downstream WHERE target_id = NEW.source_id) INTO way_back;
        IF way_back THEN
            RAISE EXCEPTION 'a link of an acyclic type cannot come back around to its source'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_cycle';
        END IF;
    END IF;
    NEW.source_reviewed_rev := src.revision;
    NEW.target_reviewed_rev := tgt.revision;
    NEW.deleted_at := NULL;
    NEW.deleted_by := NULL;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.trace_link_logged()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    t          link_type;
    source_key text;
    target_key text;
    removed    boolean := TG_OP = 'UPDATE';
    actor      uuid := CASE WHEN TG_OP = 'UPDATE' THEN NEW.deleted_by ELSE NEW.created_by END;
BEGIN
    SELECT * INTO t FROM link_type WHERE id = NEW.type_id;
    SELECT p.key || '-R' || r.number INTO source_key FROM requirement r JOIN project p ON p.id = r.project_id WHERE r.id = NEW.source_id;
    SELECT p.key || '-R' || r.number INTO target_key FROM requirement r JOIN project p ON p.id = r.project_id WHERE r.id = NEW.target_id;
    INSERT INTO requirement_change (requirement_id, field, from_value, to_value, actor_id) VALUES
        (NEW.source_id, 'link',
         CASE WHEN removed THEN t.forward || ' ' || target_key ELSE '' END,
         CASE WHEN removed THEN '' ELSE t.forward || ' ' || target_key END, actor),
        (NEW.target_id, 'link',
         CASE WHEN removed THEN t.backward || ' ' || source_key ELSE '' END,
         CASE WHEN removed THEN '' ELSE t.backward || ' ' || source_key END, actor);
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.transition_project(edge uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT w.project_id FROM workflow_transition t JOIN workflow w ON w.id = t.workflow_id WHERE t.id = edge
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.type_usage(type uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT (SELECT count(*) FROM requirement WHERE type_id = type_usage.type)::int
         + (SELECT count(*) FROM requirement_revision WHERE type_id = type_usage.type)::int
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.workflow_default_edge(flow uuid, edge_name text, from_status uuid, to_status uuid, place integer, roles jsonb, needs_comment boolean, not_author boolean)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    edge uuid;
BEGIN
    INSERT INTO workflow_transition (workflow_id, name, from_status_id, to_status_id, position)
    VALUES (flow, edge_name, from_status, to_status, place) RETURNING id INTO edge;
    INSERT INTO transition_rule (transition_id, kind, type, config, position) VALUES (edge, 'condition', 'condition.project_role', roles, 0);
    IF not_author THEN
        INSERT INTO transition_rule (transition_id, kind, type, position) VALUES (edge, 'condition', 'condition.not_author', 1);
    END IF;
    IF needs_comment THEN
        INSERT INTO transition_rule (transition_id, kind, type, position) VALUES (edge, 'validator', 'validator.comment_required', 2);
    END IF;
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION workflow_default_edge(uuid, text, uuid, uuid, integer, jsonb, boolean, boolean) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.workflow_project(flow uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT project_id FROM workflow WHERE id = flow
$function$;
-- +goose StatementEnd

DROP FUNCTION administered_without(uuid, uuid);

DROP FUNCTION project_key_claim(uuid, text, uuid, boolean);

DROP FUNCTION workflow_default_edge(uuid, uuid, text, uuid, uuid, integer, jsonb, boolean, boolean);

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.administered_without(org uuid, leaving uuid)
 RETURNS boolean
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM org_member m JOIN app_user u ON u.id = m.user_id
                   WHERE m.org_id = org AND m.user_id <> leaving AND m.org_role IN ('owner', 'admin') AND u.is_active);
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION administered_without(uuid, uuid) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.api_token_fixed()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF (NEW.org_id, NEW.user_id, NEW.name, NEW.token_hash, NEW.scopes, NEW.projects_only, NEW.expires_at, NEW.created_at)
       IS DISTINCT FROM (OLD.org_id, OLD.user_id, OLD.name, OLD.token_hash, OLD.scopes, OLD.projects_only, OLD.expires_at, OLD.created_at) THEN
        RAISE EXCEPTION 'api_token_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A token stays what it was made; make a new token instead.';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.api_token_project_addable(token uuid, project uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM api_token t
                   WHERE t.org_id = current_org_id() AND t.id = token AND t.projects_only
                     AND t.user_id = current_actor_id() AND t.created_at = now())
       AND perm_project_holds(project, current_actor_id(), 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.app_user_keeps_an_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active AND EXISTS (
        SELECT 1 FROM org_member m JOIN org o ON o.id = m.org_id
        WHERE m.user_id = NEW.id AND m.org_role IN ('owner', 'admin') AND o.archived_at IS NULL
          AND NOT administered_without(m.org_id, NEW.id)) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connected_count()
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN current_actor_administers()
                THEN (SELECT count(*)::integer FROM armature_token WHERE org_id = current_org_id())
           END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connection_forgets_links()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_remote_link WHERE org_id = OLD.org_id;
    END IF;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connection_moved()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_token WHERE org_id = OLD.org_id;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_endpoint()
 RETURNS TABLE(base_url text, armature_org_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.base_url, c.armature_org_id
    FROM armature_connection c
    WHERE c.org_id = current_org_id()
      AND EXISTS (SELECT 1 FROM org_member m WHERE m.org_id = current_org_id() AND m.user_id = current_actor_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_issue_link_open()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM armature_connection WHERE org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'the organization has not connected Armature' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM requirement WHERE org_id = NEW.org_id AND id = NEW.requirement_id
                   AND kind = 'requirement' AND trashed_at IS NULL) THEN
        RAISE EXCEPTION 'only a requirement out of the trash takes Armature issues' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit(target uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF actor IS NULL OR current_org_id() IS NULL
       OR NOT EXISTS (SELECT 1 FROM armature_connection WHERE org_id = current_org_id()) THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM armature_issue_link WHERE org_id = current_org_id() AND requirement_id = target)
       AND NOT EXISTS (SELECT 1 FROM armature_remote_link WHERE org_id = current_org_id() AND requirement_id = target) THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM outbox_event
               WHERE org_id = current_org_id() AND topic = 'armature.links' AND processed_at IS NULL
                 AND xmin = pg_current_xact_id()::xid AND payload ->> 'requirementId' = target::text) THEN
        RETURN;
    END IF;
    INSERT INTO outbox_event (org_id, topic, payload)
    VALUES (current_org_id(), 'armature.links', jsonb_build_object('requirementId', target, 'actorId', actor));
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_project_rekeyed()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM armature_links_emit(l.requirement_id)
    FROM (SELECT DISTINCT a.requirement_id FROM armature_issue_link a
          JOIN requirement r ON r.org_id = a.org_id AND r.id = a.requirement_id
          WHERE a.org_id = NEW.org_id AND r.project_id = NEW.id) l;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_kept()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement r WHERE r.org_id = OLD.org_id AND r.id = OLD.requirement_id)
       AND attachment_referenced(OLD.requirement_id, OLD.id) THEN
        RAISE EXCEPTION 'attachment_referenced' USING ERRCODE = 'check_violation',
            DETAIL = 'A revision of the requirement shows this file, so it is taken off the list instead.';
    END IF;
    RETURN OLD;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_leave_tombstone()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    INSERT INTO attachment_tombstone (object_key, org_id) VALUES (OLD.object_key, OLD.org_id)
        ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_referenced(requirement uuid, attachment uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM requirement_revision rv
        WHERE rv.org_id = current_org_id() AND rv.requirement_id = attachment_referenced.requirement
          AND jsonb_path_exists(jsonb_build_array(rv.statement, rv.rationale, rv.attributes),
                'lax $.** ? ((@.type == "image" || @.type == "attachment") && @.attrs.attachmentId == $id)',
                jsonb_build_object('id', attachment_referenced.attachment::text)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attribute_project(attribute uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT project_id FROM attribute_def WHERE id = attribute AND org_id = current_org_id()
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attribute_usage(attribute uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT (SELECT count(*) FROM requirement WHERE org_id = current_org_id() AND attributes ? attribute_usage.attribute::text)::int
         + (SELECT count(*) FROM requirement_revision WHERE org_id = current_org_id() AND attributes ? attribute_usage.attribute::text)::int
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.audit_log_prune(cutoff timestamp with time zone)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF current_org_id() IS NULL THEN
        RAISE EXCEPTION 'audit_log_prune runs inside one organization' USING ERRCODE = '42501';
    END IF;
    IF cutoff > now() - interval '1 day' THEN
        RAISE EXCEPTION 'the audit log keeps every entry for at least a day' USING ERRCODE = '22023';
    END IF;
    DELETE FROM audit_log WHERE org_id = current_org_id() AND created_at < cutoff;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.current_actor_administers()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_whole(current_actor_id()) AND EXISTS (
        SELECT 1 FROM org_member
        WHERE org_id = current_org_id() AND user_id = current_actor_id() AND org_role IN ('owner', 'admin'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.group_member_scope_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF (SELECT org_id FROM groups WHERE id = NEW.group_id) IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a group member cannot cross organizations' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member of the organization can join one of its groups' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.import_job_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF (NEW.org_id, NEW.project_id, NEW.created_by, NEW.file_name, NEW.headers, NEW.rows, NEW.total, NEW.created_at)
       IS DISTINCT FROM (OLD.org_id, OLD.project_id, OLD.created_by, OLD.file_name, OLD.headers, OLD.rows, OLD.total, OLD.created_at) THEN
        RAISE EXCEPTION 'import_job_file_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The file of an import never changes; upload it again instead.';
    END IF;
    IF OLD.state <> 'uploaded' AND (NEW.mapping, NEW.choices) IS DISTINCT FROM (OLD.mapping, OLD.choices) THEN
        RAISE EXCEPTION 'import_job_mapping_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The mapping of an import that started never changes.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'uploaded' AND NEW.state IN ('queued', 'cancelled'))
        OR (OLD.state = 'queued' AND NEW.state IN ('running', 'cancelled'))
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed', 'cancelled'))) THEN
        RAISE EXCEPTION 'import_job_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'An import moves from uploaded to queued to running, and then ends.';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_names_free()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.key <> OLD.key OR NEW.project_id IS DISTINCT FROM OLD.project_id) THEN
        RAISE EXCEPTION 'a link type keeps its key and its project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_key_fixed';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('link_type:' || NEW.org_id::text, 0));
    IF EXISTS (SELECT 1 FROM link_type t
               WHERE t.org_id = NEW.org_id AND t.id <> NEW.id AND t.key = NEW.key
                 AND (NEW.project_id IS NULL) <> (t.project_id IS NULL)) THEN
        RAISE EXCEPTION 'the link type key % is taken', NEW.key
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'link_type_key_taken';
    END IF;
    IF EXISTS (SELECT 1 FROM link_type t
               WHERE t.org_id = NEW.org_id AND t.id <> NEW.id AND lower(t.name) = lower(NEW.name)
                 AND (NEW.project_id IS NULL) <> (t.project_id IS NULL)) THEN
        RAISE EXCEPTION 'the link type name % is taken', NEW.name
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'link_type_name_taken';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_project_reach()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM link_type t WHERE t.org_id = NEW.org_id AND t.id = NEW.link_type_id
                   AND (t.project_id IS NULL OR t.project_id = NEW.project_id)) THEN
        RAISE EXCEPTION 'a project sets the organization''s link types and its own'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_rule_reach()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement_type rt
               WHERE rt.org_id = NEW.org_id AND rt.id IN (NEW.source_type_id, NEW.target_type_id) AND rt.project_id <> NEW.project_id) THEN
        RAISE EXCEPTION 'a link type rule names requirement types of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_usage(type uuid, live_only boolean)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT count(*)::int FROM trace_link
    WHERE org_id = current_org_id() AND type_id = link_type_usage.type AND (NOT live_only OR deleted_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_default_theme_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND org_id = NEW.id AND shared
    ) THEN
        RAISE EXCEPTION 'the organization''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_member_keeps_an_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.org_role IN ('owner', 'admin') AND (TG_OP = 'DELETE' OR NEW.org_role NOT IN ('owner', 'admin'))
       AND EXISTS (SELECT 1 FROM org WHERE id = OLD.org_id)
       AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND is_active)
       AND NOT administered_without(OLD.org_id, OLD.user_id) THEN
        RAISE EXCEPTION 'the installation keeps its last active administrator' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_project_holds(project uuid, actor uuid, permission text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_reaches(actor, perm_project_holds.project) AND EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.org_id = p.org_id AND m.user_id = actor
        WHERE p.id = perm_project_holds.project AND p.org_id = current_org_id()
          AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.org_role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.org_id = p.org_id AND g.project_id = p.id
                AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.org_role, g.role))))))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_project_roles(target uuid, actor uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT role), '{}') FROM (
        SELECT 'admin' AS role FROM org_member m JOIN project p ON p.org_id = m.org_id
        WHERE p.id = target AND m.user_id = actor AND m.org_role IN ('owner', 'admin')
        UNION ALL
        SELECT perm_grant_role(m.org_role, g.role) FROM project_grant g
        JOIN org_member m ON m.org_id = g.org_id AND m.user_id = actor
        WHERE g.project_id = target AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
    ) roles
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_requirement_editable(requirement uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = perm_requirement_editable.requirement AND r.org_id = current_org_id()
          AND perm_project_holds(r.project_id, actor, 'edit'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_requirement_viewable(requirement uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = perm_requirement_viewable.requirement AND r.org_id = current_org_id()
          AND perm_project_holds(r.project_id, actor, 'view')
          AND (r.trashed_at IS NULL OR perm_project_holds(r.project_id, actor, 'edit')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_grant_defaults()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.created_by AND org_role <> 'guest') THEN
        INSERT INTO project_grant (org_id, project_id, subject_type, user_id, role)
        VALUES (NEW.org_id, NEW.id, 'user', NEW.created_by, 'admin');
    END IF;
    INSERT INTO project_grant (org_id, project_id, subject_type, role) VALUES (NEW.org_id, NEW.id, 'everyone', 'editor');
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_grant_guest_check()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.role = 'admin' AND NEW.subject_type = 'user' AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id AND org_role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot be an administrator of a project. Make them an editor, or a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_grant_guest_not_admin';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_alias_check()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM project_key_claim(NEW.org_id, NEW.key, NEW.project_id, true);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_check()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM project_key_claim(NEW.org_id, NEW.key, NEW.id, false);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_claim(org uuid, claimed text, claimant uuid, as_alias boolean)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('project_key/' || org::text || '/' || claimed, 0));
    IF (as_alias AND EXISTS (SELECT 1 FROM project WHERE org_id = org AND key = claimed))
       OR (NOT as_alias AND EXISTS (SELECT 1 FROM project_key_alias WHERE org_id = org AND key = claimed AND project_id <> claimant)) THEN
        RAISE EXCEPTION 'the key % is taken by another project', claimed
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'project_key_taken';
    END IF;
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION project_key_claim(uuid, text, uuid, boolean) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_keep()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.key IS DISTINCT FROM OLD.key THEN
        DELETE FROM project_key_alias WHERE org_id = NEW.org_id AND key = NEW.key AND project_id = NEW.id;
        INSERT INTO project_key_alias (org_id, key, project_id) VALUES (OLD.org_id, OLD.key, OLD.id);
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_key_taken(claimed text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM project WHERE org_id = current_org_id() AND key = claimed)
        OR EXISTS (SELECT 1 FROM project_key_alias WHERE org_id = current_org_id() AND key = claimed)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_open(project uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM project p WHERE p.id = project_open.project AND p.org_id = current_org_id() AND p.archived_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_type_default()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    made uuid;
BEGIN
    INSERT INTO requirement_type (org_id, project_id, name, icon, colour, position)
    VALUES (NEW.org_id, NEW.id, 'Requirement', 'requirement', '#4f6bed', 0) RETURNING id INTO made;
    UPDATE project SET settings = jsonb_set(settings, '{defaultTypeId}', to_jsonb(made::text)) WHERE id = NEW.id;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.project_workflow_default(target uuid)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    org uuid;
    flow uuid;
    draft uuid; review uuid; approved uuid; implemented uuid; verified uuid; rejected uuid; obsolete uuid;
    editors jsonb := '{"roles": ["editor", "admin"]}';
    reviewers jsonb := '{"roles": ["reviewer", "admin"]}';
BEGIN
    SELECT org_id INTO org FROM project WHERE id = target;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Draft', 'draft', 0) RETURNING id INTO draft;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'In review', 'review', 1) RETURNING id INTO review;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Approved', 'approved', 2) RETURNING id INTO approved;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Implemented', 'done', 3) RETURNING id INTO implemented;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Verified', 'done', 4) RETURNING id INTO verified;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Rejected', 'closed', 5) RETURNING id INTO rejected;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Obsolete', 'closed', 6) RETURNING id INTO obsolete;
    INSERT INTO workflow (org_id, project_id, name, description)
    VALUES (org, target, 'Default workflow', 'Draft, review, approval, implementation and verification.') RETURNING id INTO flow;
    INSERT INTO workflow_step (org_id, workflow_id, status_id, is_initial, on_revision_status_id, layout_x, layout_y) VALUES
        (org, flow, draft, true, NULL, 40, 120), (org, flow, review, false, NULL, 280, 120), (org, flow, approved, false, draft, 520, 120),
        (org, flow, implemented, false, draft, 760, 120), (org, flow, verified, false, draft, 1000, 120),
        (org, flow, rejected, false, NULL, 400, 320), (org, flow, obsolete, false, NULL, 640, 320);
    PERFORM workflow_default_edge(org, flow, 'Submit for review', draft, review, 0, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Approve', review, approved, 1, reviewers, false, true);
    PERFORM workflow_default_edge(org, flow, 'Send back', review, draft, 2, reviewers, true, false);
    PERFORM workflow_default_edge(org, flow, 'Mark implemented', approved, implemented, 3, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Mark verified', implemented, verified, 4, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Reject', NULL, rejected, 5, reviewers, true, false);
    PERFORM workflow_default_edge(org, flow, 'Make obsolete', NULL, obsolete, 6, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Reopen', rejected, draft, 7, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Reopen', obsolete, draft, 8, editors, false, false);
    UPDATE project SET settings = jsonb_set(settings, '{workflowId}', to_jsonb(flow::text)) WHERE id = target;
    RETURN flow;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_attributes_check()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    project uuid;
    stray   text;
BEGIN
    IF TG_TABLE_NAME = 'requirement' THEN
        project := NEW.project_id;
    ELSE
        SELECT r.project_id INTO project FROM requirement r WHERE r.org_id = NEW.org_id AND r.id = NEW.requirement_id;
    END IF;
    IF NEW.type_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM requirement_type t WHERE t.org_id = NEW.org_id AND t.id = NEW.type_id AND t.project_id = project) THEN
        RAISE EXCEPTION 'a requirement takes a type of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_type_same_project';
    END IF;
    SELECT k INTO stray FROM jsonb_object_keys(NEW.attributes) AS k
    WHERE NOT EXISTS (SELECT 1 FROM attribute_def a WHERE a.org_id = NEW.org_id AND a.project_id = project AND a.id::text = k)
    LIMIT 1;
    IF stray IS NOT NULL THEN
        RAISE EXCEPTION 'attribute % is not one of the requirement''s project', stray
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_attributes_same_project';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_import_status(requirement uuid, status uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_import_status.requirement AND org_id = current_org_id() FOR UPDATE;
    IF r.id IS NULL OR r.kind <> 'requirement' OR r.created_at <> now() OR r.revision <> 1
       OR r.created_by IS DISTINCT FROM current_actor_id()
       OR NOT perm_project_holds(r.project_id, current_actor_id(), 'administer') THEN
        RAISE EXCEPTION 'requirement_import_status' USING ERRCODE = 'insufficient_privilege',
            DETAIL = 'An import sets the status of a requirement it has just made, and of no other.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = requirement_workflow(r.type_id, r.project_id)
                   AND status_id = requirement_import_status.status) THEN
        RAISE EXCEPTION 'requirement_import_status_unknown' USING ERRCODE = 'check_violation',
            DETAIL = 'The status is not one of the requirement''s workflow.';
    END IF;
    UPDATE requirement SET status_id = requirement_import_status.status WHERE id = r.id;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_number()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_rehome()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_remap_status(requirement uuid, status uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    flow uuid;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_remap_status.requirement AND org_id = current_org_id() FOR UPDATE;
    IF r.id IS NULL OR r.kind <> 'requirement' OR NOT perm_project_holds(r.project_id, current_actor_id(), 'edit') THEN
        RAISE EXCEPTION 'the requirement is not the actor''s to change'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_remap_allowed';
    END IF;
    flow := requirement_workflow(r.type_id, r.project_id);
    IF EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = flow AND status_id = r.status_id)
       OR NOT EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = flow AND status_id = requirement_remap_status.status) THEN
        RAISE EXCEPTION 'a status is remapped only from one the workflow lacks to one it has'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_remap_lacking';
    END IF;
    UPDATE requirement SET status_id = requirement_remap_status.status WHERE id = r.id;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_revision_next()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    current integer;
BEGIN
    SELECT revision INTO current FROM requirement WHERE org_id = NEW.org_id AND id = NEW.requirement_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'revision % of a requirement is not the one after its current revision %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_revision_in_sequence';
    END IF;
    UPDATE requirement SET revision = NEW.number WHERE org_id = NEW.org_id AND id = NEW.requirement_id;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_revision_status(requirement uuid)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    target uuid;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_revision_status.requirement AND org_id = current_org_id() FOR UPDATE;
    IF r.id IS NULL OR NOT perm_project_holds(r.project_id, current_actor_id(), 'edit') THEN
        RETURN NULL;
    END IF;
    SELECT on_revision_status_id INTO target FROM workflow_step
    WHERE workflow_id = requirement_workflow(r.type_id, r.project_id) AND status_id = r.status_id;
    IF target IS NULL OR target = r.status_id THEN
        RETURN NULL;
    END IF;
    UPDATE requirement SET status_id = target WHERE id = r.id;
    RETURN r.status_id;
END;
$function$;
-- +goose StatementEnd

DROP FUNCTION requirement_search_rows(tsquery, text, integer);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_search_rows(q tsquery, key_project text, key_number integer)
 RETURNS TABLE(id uuid, org_id uuid, project_id uuid, number integer, kind text, title text, statement jsonb, rationale jsonb, revision integer, status_id uuid, owner_id uuid, updated_at timestamp with time zone, updated_by uuid, rank real, key_match boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH keyed AS MATERIALIZED (
        SELECT r.id FROM requirement r JOIN project p ON p.org_id = r.org_id AND p.id = r.project_id
        WHERE r.org_id = current_org_id() AND p.key = key_project AND r.number = key_number
    ), hit AS MATERIALIZED (
        SELECT r.id, r.project_id, r.number, r.kind, r.title, r.statement, r.rationale, r.revision, r.status_id, r.owner_id,
               r.updated_at, r.updated_by, ts_rank(r.search_vector, q) AS rank, r.id IN (SELECT k.id FROM keyed k) AS key_match
        FROM requirement r
        WHERE r.org_id = current_org_id() AND r.trashed_at IS NULL
          AND (r.search_vector @@ q OR r.id IN (SELECT k.id FROM keyed k))
    ), judged AS MATERIALIZED (
        SELECT j.project_id, perm_project_holds(j.project_id, current_actor_id(), 'view') AS holds
        FROM (SELECT DISTINCT h.project_id FROM hit h) j
    )
    SELECT h.id, current_org_id(), h.project_id, h.number, h.kind, h.title, h.statement, h.rationale, h.revision,
           h.status_id, h.owner_id, h.updated_at, h.updated_by, h.rank, h.key_match
    FROM hit h JOIN judged j ON j.project_id = h.project_id
    WHERE j.holds
$function$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION requirement_search_rows(tsquery, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION requirement_search_rows(tsquery, text, integer) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_transition(requirement uuid, transition uuid, note text DEFAULT ''::text)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    t workflow_transition%ROWTYPE;
    actor uuid := current_actor_id();
    new_owner uuid;
    post text;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_transition.requirement AND org_id = current_org_id() FOR UPDATE;
    SELECT * INTO t FROM workflow_transition WHERE id = requirement_transition.transition AND org_id = current_org_id();
    IF r.id IS NULL OR t.id IS NULL OR r.trashed_at IS NOT NULL OR t.workflow_id <> requirement_workflow(r.type_id, r.project_id) THEN
        RAISE EXCEPTION 'that transition is not one of the requirement''s workflow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_known';
    END IF;
    IF NOT (perm_project_holds(r.project_id, actor, 'edit') OR (perm_project_holds(r.project_id, actor, 'approve') AND EXISTS (
        SELECT 1 FROM transition_rule x WHERE x.transition_id = t.id AND x.type = 'condition.project_role' AND x.config -> 'roles' ? 'reviewer'))) THEN
        RAISE EXCEPTION 'the transition is not the actor''s to take'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_transition_allowed';
    END IF;
    IF (t.from_status_id IS NOT NULL AND t.from_status_id IS DISTINCT FROM r.status_id)
       OR (t.from_status_id IS NULL AND t.to_status_id = r.status_id) THEN
        RAISE EXCEPTION 'the transition does not leave the requirement''s status'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_leaves';
    END IF;
    new_owner := r.owner_id;
    FOR post IN SELECT x.type FROM transition_rule x WHERE x.transition_id = t.id AND x.kind = 'postfunction' ORDER BY x.position LOOP
        new_owner := CASE post WHEN 'postfunction.assign_to_actor' THEN actor WHEN 'postfunction.clear_owner' THEN NULL ELSE new_owner END;
    END LOOP;
    UPDATE requirement SET status_id = t.to_status_id, owner_id = new_owner, updated_by = actor WHERE id = r.id;
    INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (r.org_id, r.id, 'status', COALESCE((SELECT name FROM req_status WHERE id = r.status_id), ''),
            (SELECT name FROM req_status WHERE id = t.to_status_id), COALESCE(note, ''), actor);
    IF new_owner IS DISTINCT FROM r.owner_id THEN
        INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, actor_id)
        VALUES (r.org_id, r.id, 'owner', COALESCE((SELECT name FROM app_user WHERE id = r.owner_id), ''),
                COALESCE((SELECT name FROM app_user WHERE id = new_owner), ''), actor);
    END IF;
    RETURN t.to_status_id;
END;
$function$;
-- +goose StatementEnd

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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.theme_in_use(theme uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT count(*)::integer FROM user_theme ut
    WHERE ut.org_id = current_org_id() AND ut.theme_id = theme_in_use.theme
      AND EXISTS (SELECT 1 FROM theme t WHERE t.org_id = ut.org_id AND t.id = ut.theme_id
                  AND (t.owner_id = current_actor_id() OR t.shared))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.theme_unshared_default()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE id = NEW.org_id AND default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.trace_link_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    t        link_type;
    src      requirement;
    tgt      requirement;
    way_back boolean;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('trace_link:' || NEW.org_id::text, 0));
    SELECT * INTO t FROM link_type WHERE org_id = NEW.org_id AND id = NEW.type_id;
    SELECT * INTO src FROM requirement WHERE org_id = NEW.org_id AND id = NEW.source_id;
    SELECT * INTO tgt FROM requirement WHERE org_id = NEW.org_id AND id = NEW.target_id;
    IF src.kind <> 'requirement' OR tgt.kind <> 'requirement' OR src.trashed_at IS NOT NULL OR tgt.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a link joins two requirements out of the trash'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_ends';
    END IF;
    IF t.retired OR (t.project_id IS NOT NULL AND t.project_id <> src.project_id) THEN
        RAISE EXCEPTION 'a link takes a live type of its source''s project or the organization'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_type';
    END IF;
    -- A type whose phrases read the same both ways is symmetric, so B to A repeats A to B.
    IF EXISTS (SELECT 1 FROM trace_link l
               WHERE l.org_id = NEW.org_id AND l.type_id = NEW.type_id AND l.deleted_at IS NULL
                 AND ((l.source_id = NEW.source_id AND l.target_id = NEW.target_id)
                      OR (t.forward = t.backward AND l.source_id = NEW.target_id AND l.target_id = NEW.source_id))) THEN
        RAISE EXCEPTION 'those requirements are already linked that way'
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'trace_link_repeat';
    END IF;
    IF (SELECT count(*) FROM trace_link l WHERE l.org_id = NEW.org_id AND l.deleted_at IS NULL
          AND (l.source_id = NEW.source_id OR l.target_id = NEW.source_id)) >= 500
       OR (SELECT count(*) FROM trace_link l WHERE l.org_id = NEW.org_id AND l.deleted_at IS NULL
          AND (l.source_id = NEW.target_id OR l.target_id = NEW.target_id)) >= 500 THEN
        RAISE EXCEPTION 'a requirement holds at most 500 links'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_limit';
    END IF;
    IF t.acyclic THEN
        WITH RECURSIVE downstream AS (
            SELECT l.target_id, ARRAY[l.target_id] AS path
            FROM trace_link l
            WHERE l.org_id = NEW.org_id AND l.type_id = NEW.type_id AND l.deleted_at IS NULL AND l.source_id = NEW.target_id
            UNION ALL
            SELECT l.target_id, d.path || l.target_id
            FROM trace_link l
            JOIN downstream d ON l.source_id = d.target_id
            WHERE l.org_id = NEW.org_id AND l.type_id = NEW.type_id AND l.deleted_at IS NULL
              AND array_length(d.path, 1) < 64 AND NOT l.target_id = ANY (d.path)
        )
        SELECT EXISTS (SELECT 1 FROM downstream WHERE target_id = NEW.source_id) INTO way_back;
        IF way_back THEN
            RAISE EXCEPTION 'a link of an acyclic type cannot come back around to its source'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_cycle';
        END IF;
    END IF;
    NEW.source_reviewed_rev := src.revision;
    NEW.target_reviewed_rev := tgt.revision;
    NEW.deleted_at := NULL;
    NEW.deleted_by := NULL;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.trace_link_logged()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    t          link_type;
    source_key text;
    target_key text;
    removed    boolean := TG_OP = 'UPDATE';
    actor      uuid := CASE WHEN TG_OP = 'UPDATE' THEN NEW.deleted_by ELSE NEW.created_by END;
BEGIN
    SELECT * INTO t FROM link_type WHERE org_id = NEW.org_id AND id = NEW.type_id;
    SELECT p.key || '-R' || r.number INTO source_key FROM requirement r JOIN project p ON p.id = r.project_id WHERE r.id = NEW.source_id;
    SELECT p.key || '-R' || r.number INTO target_key FROM requirement r JOIN project p ON p.id = r.project_id WHERE r.id = NEW.target_id;
    INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, actor_id) VALUES
        (NEW.org_id, NEW.source_id, 'link',
         CASE WHEN removed THEN t.forward || ' ' || target_key ELSE '' END,
         CASE WHEN removed THEN '' ELSE t.forward || ' ' || target_key END, actor),
        (NEW.org_id, NEW.target_id, 'link',
         CASE WHEN removed THEN t.backward || ' ' || source_key ELSE '' END,
         CASE WHEN removed THEN '' ELSE t.backward || ' ' || source_key END, actor);
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.transition_project(edge uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT w.project_id FROM workflow_transition t JOIN workflow w ON w.id = t.workflow_id WHERE t.id = edge AND t.org_id = current_org_id()
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.type_usage(type uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT (SELECT count(*) FROM requirement WHERE org_id = current_org_id() AND type_id = type_usage.type)::int
         + (SELECT count(*) FROM requirement_revision WHERE org_id = current_org_id() AND type_id = type_usage.type)::int
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.workflow_default_edge(org uuid, flow uuid, edge_name text, from_status uuid, to_status uuid, place integer, roles jsonb, needs_comment boolean, not_author boolean)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    edge uuid;
BEGIN
    INSERT INTO workflow_transition (org_id, workflow_id, name, from_status_id, to_status_id, position)
    VALUES (org, flow, edge_name, from_status, to_status, place) RETURNING id INTO edge;
    INSERT INTO transition_rule (org_id, transition_id, kind, type, config, position) VALUES (org, edge, 'condition', 'condition.project_role', roles, 0);
    IF not_author THEN
        INSERT INTO transition_rule (org_id, transition_id, kind, type, position) VALUES (org, edge, 'condition', 'condition.not_author', 1);
    END IF;
    IF needs_comment THEN
        INSERT INTO transition_rule (org_id, transition_id, kind, type, position) VALUES (org, edge, 'validator', 'validator.comment_required', 2);
    END IF;
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION workflow_default_edge(uuid, uuid, text, uuid, uuid, integer, jsonb, boolean, boolean) TO exciter_app, exciter_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.workflow_project(flow uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT project_id FROM workflow WHERE id = flow AND org_id = current_org_id()
$function$;
-- +goose StatementEnd

DROP FUNCTION administered_without(uuid);

DROP FUNCTION project_key_claim(text, uuid, boolean);

DROP FUNCTION workflow_default_edge(uuid, text, uuid, uuid, integer, jsonb, boolean, boolean);
