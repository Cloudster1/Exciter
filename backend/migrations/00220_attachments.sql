-- +goose Up
-- Files on a requirement, as Stator keeps a page's. The bytes live in the bucket under object_key; this row is
-- what is known about them and what row level security guards. No row, no way to reach the object. The key is
-- derived from the row, so no write can point a row at another organization's object, and a move to another
-- project, which keeps the requirement's id, leaves the bytes where they are.
CREATE TABLE attachment (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL,
    uploaded_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    file_name      text NOT NULL,
    content_type   text NOT NULL DEFAULT 'application/octet-stream',
    size_bytes     bigint NOT NULL,
    width          integer,
    height         integer,
    object_key     text GENERATED ALWAYS AS
        ('org/' || org_id::text || '/requirement/' || requirement_id::text || '/' || id::text) STORED,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- A file some revision shows is taken off the list instead of deleted, so history still shows it.
    deleted_at     timestamptz,
    deleted_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT attachment_name_not_blank CHECK (btrim(file_name) <> ''),
    CONSTRAINT attachment_size_positive CHECK (size_bytes > 0),
    CONSTRAINT attachment_dimensions CHECK ((width IS NULL) = (height IS NULL) AND (width IS NULL OR (width > 0 AND height > 0))),
    CONSTRAINT attachment_object_key_unique UNIQUE (object_key),
    -- A foreign key is checked past row level security, so the requirement is named with its organization and
    -- cannot be another tenant's.
    FOREIGN KEY (org_id, requirement_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE
);

CREATE INDEX attachment_requirement_idx ON attachment (org_id, requirement_id, created_at DESC);

-- A tombstone is an object whose row is gone and whose bytes may still be in the bucket, as in Stator. The
-- trigger below writes it in the statement that removes the row, whichever path removes it: a delete, a purged
-- requirement, a deleted project or organization. It is cleared once the bytes are gone. There is no foreign
-- key to org, so the tombstones of a deleted organization stay for the worker's reaper.
CREATE TABLE attachment_tombstone (
    object_key text PRIMARY KEY,
    org_id     uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- The reaper deletes whatever a tombstone names, so a tenant can only ever name objects under its own prefix.
    CONSTRAINT attachment_tombstone_own_prefix CHECK (starts_with(object_key, 'org/' || org_id::text || '/'))
);

CREATE INDEX attachment_tombstone_created_idx ON attachment_tombstone (created_at);

-- Whether a revision of the file's requirement shows it: in the statement, the rationale or a rich text value.
-- It reads every revision, whoever asks, since a revision the asker cannot see still keeps the file.
-- +goose StatementBegin
CREATE FUNCTION attachment_referenced(requirement uuid, attachment uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement_revision rv
        WHERE rv.org_id = current_org_id() AND rv.requirement_id = attachment_referenced.requirement
          AND jsonb_path_exists(jsonb_build_array(rv.statement, rv.rationale, rv.attributes),
                'lax $.** ? ((@.type == "image" || @.type == "attachment") && @.attrs.attachmentId == $id)',
                jsonb_build_object('id', attachment_referenced.attachment::text)))
$$;
-- +goose StatementEnd

-- A file a revision shows is never purged while its requirement exists. Purging the requirement takes its
-- revisions and its files together: the cascade runs after the requirement row is gone, so it passes.
-- +goose StatementBegin
CREATE FUNCTION attachment_kept() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement r WHERE r.org_id = OLD.org_id AND r.id = OLD.requirement_id)
       AND attachment_referenced(OLD.requirement_id, OLD.id) THEN
        RAISE EXCEPTION 'attachment_referenced' USING ERRCODE = 'check_violation',
            DETAIL = 'A revision of the requirement shows this file, so it is taken off the list instead.';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attachment_kept BEFORE DELETE ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_kept();

-- A file is taken off the list once, and stays what it was: the app may write only the two columns that say so.
-- Who took it off goes only with it, or to nobody when that person is removed.
-- +goose StatementBegin
CREATE FUNCTION attachment_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.deleted_at IS DISTINCT FROM OLD.deleted_at AND (OLD.deleted_at IS NOT NULL OR NEW.deleted_at IS NULL))
       OR (NEW.deleted_at IS NOT DISTINCT FROM OLD.deleted_at AND NEW.deleted_by IS NOT NULL
           AND NEW.deleted_by IS DISTINCT FROM OLD.deleted_by) THEN
        RAISE EXCEPTION 'attachment_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A file is taken off the list once, and never put back.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attachment_fixed BEFORE UPDATE ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_fixed();

-- +goose StatementBegin
CREATE FUNCTION attachment_leave_tombstone() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key, org_id) VALUES (OLD.object_key, OLD.org_id)
        ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attachment_tombstone_on_delete AFTER DELETE ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_leave_tombstone();

ALTER TABLE attachment ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachment FORCE  ROW LEVEL SECURITY;
ALTER TABLE attachment_tombstone ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachment_tombstone FORCE  ROW LEVEL SECURITY;

CREATE POLICY attachment_tenant_isolation ON attachment
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attachment_tombstone_tenant_isolation ON attachment_tombstone
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attachment_admin_bypass ON attachment TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY attachment_tombstone_admin_bypass ON attachment_tombstone TO exciter_admin USING (true) WITH CHECK (true);

-- A file is its requirement's: seen with it, and uploaded, taken off the list and deleted by who edits it.
CREATE POLICY attachment_visible ON attachment AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY attachment_uploaded_by_editors ON attachment AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (uploaded_by = current_actor_id() AND deleted_at IS NULL AND deleted_by IS NULL
                AND perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY attachment_taken_off_by_editors ON attachment AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()))
    WITH CHECK (deleted_by = current_actor_id());
CREATE POLICY attachment_deleted_by_editors ON attachment AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()));

-- A file never changes once stored; a new one is uploaded instead. Without UPDATE on the rest the app cannot
-- move a row, and with it its key, off its object. The schema's default privileges grant it, so it is taken back.
GRANT SELECT, INSERT, UPDATE, DELETE ON attachment, attachment_tombstone TO exciter_admin;
REVOKE ALL ON attachment, attachment_tombstone FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON attachment, attachment_tombstone TO exciter_app;
GRANT UPDATE (deleted_at, deleted_by) ON attachment TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS attachment;
DROP FUNCTION IF EXISTS attachment_leave_tombstone();
DROP FUNCTION IF EXISTS attachment_fixed();
DROP FUNCTION IF EXISTS attachment_kept();
DROP FUNCTION IF EXISTS attachment_referenced(uuid, uuid);
DROP TABLE IF EXISTS attachment_tombstone;
