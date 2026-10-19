package reqifio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/version"
)

// ErrNoSection is an export of a section the project does not have.
var ErrNoSection = errors.New("no such requirement to export")

// Exported says how much of the project an export wrote, and under what name.
type Exported struct {
	Name    string
	Rows    int
	Matched int
	// FilesLeft counts the files past MaxExportImageBytes the archive left out.
	FilesLeft int
}

// Truncated says the export left something out.
func (e Exported) Truncated() bool { return e.Rows < e.Matched || e.FilesLeft > 0 }

// content is what an export reads of one requirement besides its place in the tree.
type content struct {
	Statement  json.RawMessage
	Rationale  json.RawMessage
	Attributes map[string]json.RawMessage
	Status     *uuid.UUID
	Owner      string
	Priority   *requirement.Priority
	Labels     []string
	Updated    time.Time
	Test       requirement.TestCase
	Risk       requirement.Rating
}

// snapshot is the part of a project an export writes, read in one transaction.
type snapshot struct {
	project  *project.Project
	nodes    []requirement.TreeNode
	contents map[uuid.UUID]content
	defs     []attribute.Def
	types    map[uuid.UUID]named
	// testTypes are the types marked as test cases, whose objects carry what a test case does (#250).
	testTypes map[uuid.UUID]bool
	// riskTypes are the types marked as risks, whose objects carry a rating (#259).
	riskTypes map[uuid.UUID]bool
	changed   map[uuid.UUID]time.Time
	statuses  []named
	links     []exportLink
	kinds     map[uuid.UUID]linkType
	people    map[string]string
	ties      map[string]map[uuid.UUID][]tieRow
	// root is the section a section export starts at, nil for the whole project.
	root *requirement.TreeNode
	// glossary is the terms the export defines: the project's, or those a section's requirements use.
	glossary []specdoc.GlossaryTerm
	// now, exportID and tool are what the export says of itself, set by Export and fixed by the golden test.
	now      time.Time
	exportID string
	tool     string
}

type exportLink struct {
	ID, Type, Source, Target uuid.UUID
	Created                  time.Time
}

// Export writes a project, or a requirement and everything below it, as ReqIF; an archive packs the files its
// text shows beside the document.
func (s *Service) Export(ctx context.Context, w io.Writer, actor uuid.UUID, projectKey, section string, archive bool) (Exported, error) {
	var done Exported
	nodes, err := s.requirements.Tree(ctx, actor, projectKey)
	if err != nil {
		return done, err
	}
	var root *requirement.TreeNode
	if section != "" {
		if nodes, root = subtree(nodes, section); root == nil {
			return done, ErrNoSection
		}
	}
	done.Matched = len(nodes)
	if len(nodes) > MaxExportRows {
		nodes = nodes[:MaxExportRows]
	}
	done.Rows = len(nodes)
	snap := &snapshot{nodes: nodes, root: root, now: time.Now().UTC(), exportID: uuid.NewString(), tool: "Exciter " + version.Current().Version}
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error { return snap.read(ctx, tx, actor, projectKey) }); err != nil {
		return done, err
	}
	name := requirement.NormalizedProjectKey(projectKey)
	if root != nil {
		name = root.Key
	}
	files := map[uuid.UUID]*attachment.Located{}
	var fileRef FileRef
	if archive {
		fileRef, done.FilesLeft = s.locate(ctx, actor, snap, files)
	}
	f := snap.build(fileRef)
	if !archive {
		done.Name = name + ".reqif"
		return done, Write(w, f)
	}
	done.Name = name + ".reqifz"
	return done, writeArchive(ctx, w, name+".reqif", f, files)
}

// subtree is the node of the key and everything below it, in reading order.
func subtree(nodes []requirement.TreeNode, key string) ([]requirement.TreeNode, *requirement.TreeNode) {
	for i := range nodes {
		if !strings.EqualFold(nodes[i].Key, strings.TrimSpace(key)) {
			continue
		}
		end := i + 1
		for end < len(nodes) && nodes[end].Depth > nodes[i].Depth {
			end++
		}
		root := nodes[i]
		return nodes[i:end], &root
	}
	return nil, nil
}

func (snap *snapshot) read(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) error {
	p, err := project.Find(ctx, tx, actor, projectKey)
	if err != nil {
		return err
	}
	snap.project = p
	if snap.defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
		return err
	}
	ids := make([]uuid.UUID, len(snap.nodes))
	for i, n := range snap.nodes {
		ids[i] = n.ID
	}
	for _, read := range []func(context.Context, db.DBTX, []uuid.UUID) error{snap.readContents, snap.readSchema, snap.readLinks, snap.readPeople, snap.readTies} {
		if err := read(ctx, tx, ids); err != nil {
			return err
		}
	}
	if snap.root != nil {
		snap.glossary, err = specdoc.UsedGlossary(ctx, tx, ids)
	} else {
		snap.glossary, err = specdoc.ProjectGlossary(ctx, tx, p.ID)
	}
	return err
}

func (snap *snapshot) readContents(ctx context.Context, tx db.DBTX, ids []uuid.UUID) error {
	snap.contents = map[uuid.UUID]content{}
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.statement, r.rationale, r.attributes || requirement_computed_of(r.id), r.status_id, COALESCE(lower(u.email::text), ''), r.priority, r.updated_at,
		       COALESCE((SELECT array_agg(l.name ORDER BY l.name) FROM requirement_label l WHERE l.requirement_id = r.id), '{}'),
		       r.preconditions, r.steps, r.parameters, r.risk
		FROM requirement r LEFT JOIN app_user u ON u.id = r.owner_id
		WHERE r.id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id                 uuid.UUID
			c                  content
			attrs              []byte
			pre, steps, params []byte
			rating             []byte
		)
		if err := rows.Scan(&id, &c.Statement, &c.Rationale, &attrs, &c.Status, &c.Owner, &c.Priority, &c.Updated, &c.Labels, &pre, &steps, &params,
			&rating); err != nil {
			return err
		}
		var err error
		if c.Test, err = requirement.ReadTestCase(pre, steps, params); err != nil {
			return err
		}
		if c.Risk, err = requirement.ReadRating(rating); err != nil {
			return err
		}
		if err := json.Unmarshal(attrs, &c.Attributes); err != nil {
			return err
		}
		snap.contents[id] = c
	}
	return rows.Err()
}

func (snap *snapshot) readSchema(ctx context.Context, tx db.DBTX, _ []uuid.UUID) error {
	snap.types, snap.changed, snap.testTypes, snap.riskTypes = map[uuid.UUID]named{}, map[uuid.UUID]time.Time{}, map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	rows, err := tx.Query(ctx, `SELECT id, name, updated_at, role = 'testCase', role = 'risk' FROM requirement_type WHERE project_id = $1`, snap.project.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			t    named
			at   time.Time
			test bool
			risk bool
		)
		if err := rows.Scan(&t.ID, &t.Name, &at, &test, &risk); err != nil {
			rows.Close()
			return err
		}
		snap.types[t.ID], snap.changed[t.ID], snap.testTypes[t.ID], snap.riskTypes[t.ID] = t, at, test, risk
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	statuses, err := tx.Query(ctx, `SELECT id, name FROM req_status WHERE project_id = $1 ORDER BY position, name`, snap.project.ID)
	if err != nil {
		return err
	}
	snap.statuses, err = pgx.CollectRows(statuses, func(row pgx.CollectableRow) (named, error) {
		var s named
		return s, row.Scan(&s.ID, &s.Name)
	})
	return err
}

func (snap *snapshot) readLinks(ctx context.Context, tx db.DBTX, ids []uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT l.id, l.type_id, l.source_id, l.target_id, l.created_at, t.key, t.name FROM trace_link l
		JOIN link_type t ON t.id = l.type_id
		WHERE l.deleted_at IS NULL AND l.source_id = ANY($1) AND l.target_id = ANY($1)
		ORDER BY l.created_at, l.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	snap.kinds = map[uuid.UUID]linkType{}
	for rows.Next() {
		var (
			l  exportLink
			lt linkType
		)
		if err := rows.Scan(&l.ID, &l.Type, &l.Source, &l.Target, &l.Created, &lt.Key, &lt.Name); err != nil {
			return err
		}
		lt.ID = l.Type
		snap.links, snap.kinds[l.Type] = append(snap.links, l), lt
	}
	return rows.Err()
}

// readPeople reads the emails of the people attributes name, which an export writes for them.
func (snap *snapshot) readPeople(ctx context.Context, tx db.DBTX, _ []uuid.UUID) error {
	snap.people = map[string]string{}
	var ids []string
	for _, d := range snap.defs {
		if d.Kind != attribute.User {
			continue
		}
		for _, c := range snap.contents {
			var id string
			if json.Unmarshal(c.Attributes[d.ID.String()], &id) == nil && id != "" {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text, lower(email::text) FROM app_user WHERE id::text = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			return err
		}
		snap.people[id] = email
	}
	return rows.Err()
}

func (snap *snapshot) readTies(ctx context.Context, tx db.DBTX, _ []uuid.UUID) error {
	snap.ties = map[string]map[uuid.UUID][]tieRow{}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (kind, target_id, scope_id) kind, identifier, target_id, scope_id FROM reqif_identity
		WHERE project_id = $1 ORDER BY kind, target_id, scope_id, created_at DESC`, snap.project.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var t tieRow
		if err := rows.Scan(&t.kind, &t.identifier, &t.target, &t.scope); err != nil {
			return err
		}
		if snap.ties[t.kind] == nil {
			snap.ties[t.kind] = map[uuid.UUID][]tieRow{}
		}
		snap.ties[t.kind][t.target] = append(snap.ties[t.kind][t.target], t)
	}
	return rows.Err()
}

// identifier is what the file calls something: the identifier an exchange tied to it, else the fallback.
func (snap *snapshot) identifier(kind string, target uuid.UUID, scope *uuid.UUID, fallback string) string {
	for _, t := range snap.ties[kind][target] {
		if (t.scope == nil && scope == nil) || (t.scope != nil && scope != nil && *t.scope == *scope) {
			return t.identifier
		}
	}
	return fallback
}

// fileExtensions name the files an archive packs by their type.
var fileExtensions = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "application/pdf": ".pdf", "text/plain": ".txt",
}

// locate finds the files the exported text shows, up to MaxExportImageBytes, and says how many it left out.
func (s *Service) locate(ctx context.Context, actor uuid.UUID, snap *snapshot, files map[uuid.UUID]*attachment.Located) (FileRef, int) {
	var (
		total int64
		left  int
		seen  = map[uuid.UUID]bool{}
	)
	for _, n := range snap.nodes {
		c := snap.contents[n.ID]
		docs := []json.RawMessage{c.Statement, c.Rationale}
		for _, d := range snap.defs {
			if d.Kind == attribute.RichText {
				docs = append(docs, c.Attributes[d.ID.String()])
			}
		}
		for _, id := range fileIDs(docs) {
			if seen[id] {
				continue
			}
			seen[id] = true
			located, err := s.files.Locate(ctx, actor, id)
			if err != nil {
				continue
			}
			if total+located.Size > MaxExportImageBytes {
				left++
				continue
			}
			total += located.Size
			files[id] = located
		}
	}
	return func(id string) (string, string, bool) {
		parsed, err := uuid.Parse(id)
		if err != nil || files[parsed] == nil {
			return "", "", false
		}
		ext, ok := fileExtensions[files[parsed].ContentType]
		if !ok {
			ext = ".bin"
		}
		return "files/" + parsed.String() + ext, files[parsed].ContentType, true
	}, left
}

// fileIDs are the attachments documents show, in the order they are met.
func fileIDs(docs []json.RawMessage) []uuid.UUID {
	var ids []uuid.UUID
	var walk func(document.Node)
	walk = func(n document.Node) {
		if n.Type == document.NodeImage || n.Type == document.NodeAttachment {
			if s, ok := n.Attrs["attachmentId"].(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					ids = append(ids, id)
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, raw := range docs {
		var root document.Node
		if len(raw) > 0 && json.Unmarshal(raw, &root) == nil {
			walk(root)
		}
	}
	return ids
}

// writeArchive writes a .reqifz: the document, then the files it shows under files/.
func writeArchive(ctx context.Context, w io.Writer, name string, f *File, files map[uuid.UUID]*attachment.Located) error {
	var doc bytes.Buffer
	if err := Write(&doc, f); err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	entry, err := zw.Create(name)
	if err != nil {
		return err
	}
	if _, err := entry.Write(doc.Bytes()); err != nil {
		return err
	}
	for id, located := range files {
		ext, ok := fileExtensions[located.ContentType]
		if !ok {
			ext = ".bin"
		}
		body, err := located.Bytes(ctx)
		if err != nil {
			return err
		}
		entry, err := zw.Create("files/" + id.String() + ext)
		if err == nil {
			_, err = io.Copy(entry, body)
		}
		body.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}
