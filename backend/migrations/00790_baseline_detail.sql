-- +goose Up
-- What a baseline records for its matrix (#153), as docs/api-contract-m5.md agrees: from now on each item's type,
-- whether each trace link was suspect and its type's key, each Armature link's status and whether it was suspect,
-- and the Stator page links. detail says how much a baseline recorded: 1 for one taken before, 2 from now on, and
-- null in a column of a detail 1 baseline means "not recorded", never "no". The app role updates none of it: the
-- column grants of 00550 name only what a baseline's keeper may change.
ALTER TABLE baseline ADD COLUMN detail smallint NOT NULL DEFAULT 1 CHECK (detail IN (1, 2));
ALTER TABLE baseline_item ADD COLUMN type_id uuid;
ALTER TABLE baseline_link ADD COLUMN type_key text;
ALTER TABLE baseline_link ADD COLUMN suspect boolean;
ALTER TABLE baseline_external_link ADD COLUMN status_category text CHECK (status_category IN ('todo', 'in_progress', 'done'));
ALTER TABLE baseline_external_link ADD COLUMN suspect boolean;

CREATE TABLE baseline_page_link (
    baseline_id    uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    link_id        uuid NOT NULL,
    requirement_id uuid NOT NULL,
    relation       text NOT NULL,
    page_id        uuid NOT NULL,
    space_key      text NOT NULL,
    url            text NOT NULL,
    suspect        boolean NOT NULL,
    PRIMARY KEY (baseline_id, link_id)
);

CREATE TRIGGER baseline_page_link_frozen BEFORE INSERT OR UPDATE OR DELETE ON baseline_page_link
    FOR EACH ROW EXECUTE FUNCTION baseline_rows_frozen();

ALTER TABLE baseline_page_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_page_link FORCE  ROW LEVEL SECURITY;
CREATE POLICY baseline_page_link_installation ON baseline_page_link USING (true) WITH CHECK (true);
-- As the other rows a baseline holds: shown with it, written by whoever took it.
CREATE POLICY baseline_page_link_readers ON baseline_page_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_page_link_takers ON baseline_page_link AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id AND b.created_by = current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON baseline_page_link TO exciter_admin;
REVOKE ALL ON baseline_page_link FROM exciter_app;
GRANT SELECT, INSERT ON baseline_page_link TO exciter_app;

-- +goose Down
DROP TABLE baseline_page_link;
ALTER TABLE baseline_external_link DROP COLUMN suspect;
ALTER TABLE baseline_external_link DROP COLUMN status_category;
ALTER TABLE baseline_link DROP COLUMN suspect;
ALTER TABLE baseline_link DROP COLUMN type_key;
ALTER TABLE baseline_item DROP COLUMN type_id;
ALTER TABLE baseline DROP COLUMN detail;
