package specdoc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Service reads documents as their reader may see them.
type Service struct {
	db           *db.Cluster
	requirements *requirement.Service
	baselines    *baseline.Service
}

func NewService(cluster *db.Cluster, requirements *requirement.Service, baselines *baseline.Service) *Service {
	return &Service{db: cluster, requirements: requirements, baselines: baselines}
}

// Project reads a project's specification as it reads now, or with section a requirement and what is below it.
func (s *Service) Project(ctx context.Context, reader uuid.UUID, projectKey, section string) (*Document, error) {
	nodes, err := s.requirements.Tree(ctx, reader, projectKey)
	if err != nil {
		return nil, err
	}
	doc := &Document{Kind: KindProject, ExportedAt: time.Now().UTC(), Entries: []Entry{}}
	if section != "" {
		start := -1
		for i := range nodes {
			if strings.EqualFold(nodes[i].Key, strings.TrimSpace(section)) {
				start = i
				break
			}
		}
		if start < 0 {
			return nil, ErrNoSection
		}
		end := start + 1
		for end < len(nodes) && nodes[end].Depth > nodes[start].Depth {
			end++
		}
		top := nodes[start].Depth
		nodes = nodes[start:end]
		for i := range nodes {
			nodes[i].Depth -= top
		}
		doc.Kind, doc.SectionKey, doc.Title = KindSection, nodes[0].Key, nodes[0].Title
	}
	doc.Total = len(nodes)
	if len(nodes) > MaxEntries {
		nodes = nodes[:MaxEntries]
	}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		doc.ProjectKey, doc.ProjectName = p.Key, p.Name
		if doc.Title == "" {
			doc.Title = p.Name
		}
		if doc.ExportedBy, err = nameOf(ctx, tx, reader); err != nil {
			return err
		}
		r := &reading{ctx: ctx, tx: tx}
		if r.defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(nodes))
		for i, n := range nodes {
			ids[i] = n.ID
			doc.Entries = append(doc.Entries, Entry{ID: n.ID, Key: n.Key, Kind: n.Kind, Outline: n.Outline, Depth: n.Depth, Title: n.Title,
				Revision: n.Revision, Properties: []Property{}, Links: []Link{}})
		}
		if err := r.contents(doc, ids); err != nil {
			return err
		}
		return r.links(doc, ids, liveLinks)
	})
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// Baseline reads a baseline as it was taken: each requirement at the revision it holds, with the links it held.
// Owner, priority and labels are left out, since a baseline keeps none of them.
func (s *Service) Baseline(ctx context.Context, reader, id uuid.UUID) (*Document, error) {
	b, err := s.baselines.Get(ctx, reader, id)
	if err != nil {
		return nil, err
	}
	doc := &Document{Kind: KindBaseline, Title: b.Name, ProjectKey: b.ProjectKey, ExportedAt: time.Now().UTC(), Entries: []Entry{}, Total: b.Items,
		Baseline: &BaselineInfo{ID: b.ID, Name: b.Name, Description: b.Description, TakenAt: b.CreatedAt, TakenBy: b.CreatedByName, LockedAt: b.LockedAt}}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, b.ProjectKey)
		if err != nil {
			return err
		}
		doc.ProjectName = p.Name
		if doc.ExportedBy, err = nameOf(ctx, tx, reader); err != nil {
			return err
		}
		r := &reading{ctx: ctx, tx: tx}
		if r.defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
			return err
		}
		ids, err := r.baselineItems(doc, b.ID, p.Key)
		if err != nil {
			return err
		}
		return r.links(doc, ids, func(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (rowsOf, error) {
			return tx.Query(ctx, baselineLinks, ids, b.ID)
		})
	})
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func nameOf(ctx context.Context, tx db.DBTX, id uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(name, ''), email::text) FROM app_user WHERE id = $1`, id).Scan(&name)
	return name, err
}

// reading is one document being read in one transaction.
type reading struct {
	ctx  context.Context
	tx   db.DBTX
	defs []attribute.Def
	// people are the names of the people user attributes name, read once.
	people map[string]string
}

func (r *reading) contents(doc *Document, ids []uuid.UUID) error {
	rows, err := r.tx.Query(r.ctx, `
		SELECT r.id, r.statement, r.rationale, r.attributes, COALESCE(t.name, ''), COALESCE(st.name, ''),
		       COALESCE(NULLIF(u.name, ''), u.email::text, ''), COALESCE(r.priority, ''),
		       COALESCE((SELECT array_agg(l.name ORDER BY l.name) FROM requirement_label l WHERE l.requirement_id = r.id), '{}')
		FROM requirement r
		LEFT JOIN requirement_type t ON t.id = r.type_id
		LEFT JOIN req_status st ON st.id = r.status_id
		LEFT JOIN app_user u ON u.id = r.owner_id
		WHERE r.id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	type found struct {
		statement, rationale, attributes  []byte
		typeName, status, owner, priority string
		labels                            []string
	}
	byID := map[uuid.UUID]found{}
	for rows.Next() {
		var (
			id uuid.UUID
			f  found
		)
		if err := rows.Scan(&id, &f.statement, &f.rationale, &f.attributes, &f.typeName, &f.status, &f.owner, &f.priority, &f.labels); err != nil {
			rows.Close()
			return err
		}
		byID[id] = f
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range doc.Entries {
		e := &doc.Entries[i]
		f := byID[e.ID]
		e.Statement, e.Rationale = document(f.statement), rationaleOf(f.rationale)
		if e.Kind != requirement.KindRequirement {
			continue
		}
		e.Properties = appendText(e.Properties, FieldType, f.typeName)
		e.Properties = appendText(e.Properties, FieldStatus, f.status)
		e.Properties = appendText(e.Properties, FieldOwner, f.owner)
		e.Properties = appendText(e.Properties, FieldPriority, f.priority)
		e.Properties = appendText(e.Properties, FieldLabels, strings.Join(f.labels, ", "))
		attrs, err := r.attributes(f.attributes)
		if err != nil {
			return err
		}
		e.Properties = append(e.Properties, attrs...)
	}
	return nil
}

func appendText(props []Property, field, text string) []Property {
	if text == "" {
		return props
	}
	return append(props, Property{Field: field, Text: text})
}

// document is a stored document, the empty one when there is none.
func document(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{"type":"doc","content":[]}`)
	}
	return raw
}

func rationaleOf(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}
