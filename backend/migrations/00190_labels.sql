-- +goose Up
-- Labels are the organization's words on requirements, as Stator's are on pages. A label has no row of its
-- own: it exists while a requirement carries it, so nobody learns a word from a requirement they may not view.
-- Names are stored as label.Normalize writes them; the check below is the part of that rule the database can
-- hold without depending on its locale.
CREATE TABLE requirement_label (
    org_id         uuid NOT NULL,
    requirement_id uuid NOT NULL,
    name           text NOT NULL,
    created_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, requirement_id, name),
    CONSTRAINT requirement_label_name_normal CHECK (
        char_length(name) BETWEEN 1 AND 40
        AND name = lower(name)
        AND name !~ '^[-_.]'
        AND name !~ '[[:space:][:cntrl:]!"#$%&''()*+,/:;<=>?@\[\\\]^`{|}~]'),
    FOREIGN KEY (org_id, requirement_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE
);

-- text_pattern_ops serves both a label's requirements and the prefix autocomplete asks for.
CREATE INDEX requirement_label_name_idx ON requirement_label (org_id, name text_pattern_ops, requirement_id);

ALTER TABLE requirement_label ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_label FORCE  ROW LEVEL SECURITY;

CREATE POLICY requirement_label_tenant_isolation ON requirement_label
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY requirement_label_admin_bypass ON requirement_label TO exciter_admin USING (true) WITH CHECK (true);

-- A label is its requirement's: seen with it, put on and taken off by who edits it.
CREATE POLICY requirement_label_visible ON requirement_label AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY requirement_label_added_by_editors ON requirement_label AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY requirement_label_removed_by_editors ON requirement_label AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()));

-- A label is put on or taken off, never changed in place: the schema's default privileges would give the app
-- role UPDATE too.
GRANT SELECT, INSERT, UPDATE, DELETE ON requirement_label TO exciter_admin;
REVOKE ALL ON requirement_label FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON requirement_label TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS requirement_label;
