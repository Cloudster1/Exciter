package baseline

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/signing"
)

var (
	// ErrSigned is a rename or a delete of a signed baseline, which keeps its name and is never deleted.
	ErrSigned = errors.New("the baseline is signed")
	// ErrNoSigningRule is a signature in a project where nobody signs baselines.
	ErrNoSigningRule = errors.New("nobody signs this project's baselines")
	// ErrNotSigner is a signature by somebody the baseline's signers do not name.
	ErrNotSigner = errors.New("not one of the baseline's signers")
	// ErrAlreadySigned is a second signature of the same signer.
	ErrAlreadySigned = errors.New("already signed")
	// ErrChanged is a meaning other than the one the baseline says now, read before something changed.
	ErrChanged = errors.New("the baseline changed since it was read")
)

// WithPasswords lets the service check a signer's password; without it only a re-authentication signs.
func (s *Service) WithPasswords(p signing.Passwords) *Service {
	s.passwords = p
	return s
}

// meaning is the sentence a baseline's signature means, which the signer confirms and the signature keeps.
func meaning(lang, name, projectKey string) string {
	if lang == "de" {
		return fmt.Sprintf("Ich gebe die Baseline %s von %s als vereinbarten Stand ihrer Anforderungen frei.", name, projectKey)
	}
	return fmt.Sprintf("I release the baseline %s of %s as the agreed state of its requirements.", name, projectKey)
}

// fillSigning puts on each baseline where its signing stands, with who signs it: the signers it froze, or the
// project's until it froze them.
func fillSigning(ctx context.Context, tx db.DBTX, all []found) error {
	var frozen, projects []uuid.UUID
	for _, f := range all {
		if f.frozen != nil {
			frozen = append(frozen, f.ID)
		} else if f.rule != nil && !slices.Contains(projects, f.projectID) {
			projects = append(projects, f.projectID)
		}
	}
	byBaseline, err := signers(ctx, tx, "baseline_signer", "baseline_id", frozen)
	if err != nil {
		return err
	}
	byProject, err := signers(ctx, tx, "baseline_signing_rule_signer", "project_id", projects)
	if err != nil {
		return err
	}
	session, lang := signing.InSession(ctx), signing.Lang(ctx)
	for i := range all {
		f := &all[i]
		switch {
		case f.frozen != nil:
			f.Signing = &Signing{Required: *f.frozen, Signers: byBaseline[f.ID], Frozen: true}
		case f.rule != nil:
			f.Signing = &Signing{Required: *f.rule, Signers: byProject[f.projectID]}
		default:
			continue
		}
		f.Signing.Signatures, f.Signing.Signed = f.signatures, f.signatures >= f.Signing.Required
		if f.Signing.Signers == nil {
			f.Signing.Signers = []Signer{}
		}
		if f.maySign && session {
			f.Signing.CanSign, f.Signing.Meaning = true, meaning(lang, f.Name, f.ProjectKey)
		}
	}
	return nil
}

// signers reads the people and groups of a signing rule or of what baselines froze, by their owner's id.
func signers(ctx context.Context, tx db.DBTX, table, owner string, ids []uuid.UUID) (map[uuid.UUID][]Signer, error) {
	out := map[uuid.UUID][]Signer{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT s.`+owner+`, s.subject_type, s.user_id, s.group_id, COALESCE(u.name, g.name, '')
		FROM `+table+` s LEFT JOIN app_user u ON u.id = s.user_id LEFT JOIN groups g ON g.id = s.group_id
		WHERE s.`+owner+` = ANY($1)
		ORDER BY s.subject_type DESC, lower(COALESCE(u.name, g.name, '')), s.user_id, s.group_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("read who signs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id uuid.UUID
			s  Signer
		)
		if err := rows.Scan(&id, &s.Type, &s.UserID, &s.GroupID, &s.Name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], s)
	}
	return out, rows.Err()
}

// SigningRule answers who signs the project's baselines, nil when nobody does.
func (s *Service) SigningRule(ctx context.Context, reader uuid.UUID, projectKey string) (*SigningRule, error) {
	var out *SigningRule
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = readRule(ctx, tx, p.ID)
		return err
	})
	return out, err
}

func readRule(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*SigningRule, error) {
	var r SigningRule
	err := tx.QueryRow(ctx, `SELECT required, updated_at FROM baseline_signing_rule WHERE project_id = $1`, projectID).
		Scan(&r.Required, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the signing rule: %w", err)
	}
	by, err := signers(ctx, tx, "baseline_signing_rule_signer", "project_id", []uuid.UUID{projectID})
	r.Signers = by[projectID]
	if r.Signers == nil {
		r.Signers = []Signer{}
	}
	return &r, err
}

// checkRule holds a signing rule to people who are members and groups that exist, each once, and to no more
// signatures than people when only people are named.
func checkRule(ctx context.Context, tx db.DBTX, in SigningRuleInput) error {
	if in.Required < MinSignatures || in.Required > MaxSignatures {
		return &FieldError{"required", fmt.Sprintf("Ask for %d to %d signatures.", MinSignatures, MaxSignatures)}
	}
	if len(in.Signers) == 0 || len(in.Signers) > MaxSigners {
		return &FieldError{"signers", fmt.Sprintf("Name 1 to %d people or groups who sign.", MaxSigners)}
	}
	people := 0
	seen := map[string]bool{}
	for _, a := range in.Signers {
		var (
			id    *uuid.UUID
			query string
		)
		switch a.Type {
		case SignerUser:
			id, query = a.UserID, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL AND is_active)`
			people++
		case SignerGroup:
			id, query = a.GroupID, `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1)`
		default:
			return &FieldError{"signers", "A signer is a person or a group; everyone cannot sign."}
		}
		if id == nil {
			return &FieldError{"signers", "Name each signer by the id of the person or the group."}
		}
		key := string(a.Type) + id.String()
		if seen[key] {
			return &FieldError{"signers", "Name each signer once."}
		}
		seen[key] = true
		var exists bool
		if err := tx.QueryRow(ctx, query, *id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &FieldError{"signers", "One of the signers is no member or group here. Pick them again from the list."}
		}
	}
	if people == len(in.Signers) && in.Required > people {
		return &FieldError{"required", fmt.Sprintf("Ask for at most %d signatures, one per person named, or name a group.", people)}
	}
	return nil
}

// SaveSigningRule sets who signs the project's baselines; a baseline already being signed keeps the rule it froze.
func (s *Service) SaveSigningRule(ctx context.Context, actor uuid.UUID, projectKey string, in SigningRuleInput) (*SigningRule, db.LSN, error) {
	var out *SigningRule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := keeping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if err := checkRule(ctx, tx, in); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO baseline_signing_rule (project_id, required, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (project_id) DO UPDATE SET required = EXCLUDED.required, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			p.ID, in.Required, actor); err != nil {
			return fmt.Errorf("save the signing rule: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM baseline_signing_rule_signer WHERE project_id = $1`, p.ID); err != nil {
			return err
		}
		for _, a := range in.Signers {
			if _, err := tx.Exec(ctx, `
				INSERT INTO baseline_signing_rule_signer (project_id, subject_type, user_id, group_id) VALUES ($1, $2, $3, $4)`,
				p.ID, a.Type, a.UserID, a.GroupID); err != nil {
				return fmt.Errorf("name who signs: %w", err)
			}
		}
		if out, err = readRule(ctx, tx, p.ID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineSigningSaved, TargetType: "project", TargetID: &p.ID,
			Actor: actor, Data: map[string]any{"project": p.Key, "required": in.Required, "signers": len(in.Signers)}})
	})
	return out, lsn, err
}

// DeleteSigningRule stops the project's baselines from being signed from now on; signatures given stay.
func (s *Service) DeleteSigningRule(ctx context.Context, actor uuid.UUID, projectKey string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := keeping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM baseline_signing_rule WHERE project_id = $1`, p.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineSigningRemoved, TargetType: "project", TargetID: &p.ID,
			Actor: actor, Data: map[string]any{"project": p.Key}})
	})
}

// Signatures answers every signature of a baseline, in the order they were given.
func (s *Service) Signatures(ctx context.Context, reader, id uuid.UUID) ([]Signature, error) {
	out := []Signature{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := get(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT COALESCE(u.name, ''), s.meaning, s.method, s.authenticated_at, s.signed_at, s.content_hash
			FROM baseline_signature s LEFT JOIN app_user u ON u.id = s.signer_id
			WHERE s.baseline_id = $1 ORDER BY s.signed_at, s.id`, id)
		if err != nil {
			return fmt.Errorf("read the signatures: %w", err)
		}
		all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Signature, error) {
			var g Signature
			err := row.Scan(&g.SignerName, &g.Meaning, &g.Method, &g.AuthenticatedAt, &g.SignedAt, &g.ContentHash)
			return g, err
		})
		if all != nil {
			out = all
		}
		return err
	})
	return out, err
}

// Sign gives the caller's signature of a baseline with the meaning the dialog showed, proven by their password or a
// fresh sign-in, over the hash of what the baseline holds; the database checks each of it again.
func (s *Service) Sign(ctx context.Context, actor, id uuid.UUID, in SignInput) (*Baseline, db.LSN, error) {
	if err := s.prove(ctx, actor, in); err != nil {
		return nil, 0, err
	}
	var out *Baseline
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		var archived bool
		if err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM project WHERE id = $1`, f.projectID).Scan(&archived); err != nil {
			return err
		}
		switch {
		case archived:
			return project.ErrArchived
		case f.Signing == nil:
			return ErrNoSigningRule
		case !f.Signing.CanSign:
			var mine bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM baseline_signature WHERE baseline_id = $1 AND signer_id = $2)`,
				id, actor).Scan(&mine); err != nil {
				return err
			}
			if mine {
				return ErrAlreadySigned
			}
			return ErrNotSigner
		case strings.TrimSpace(in.Meaning) != f.Signing.Meaning:
			return ErrChanged
		}
		content, err := readContent(ctx, tx, f)
		if err != nil {
			return err
		}
		proof, err := signing.Spend(ctx, tx, actor, in.Password != "", in.Reauthentication)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO baseline_signature (baseline_id, content_hash, signer_id, meaning, method, authenticated_at, session_id,
			                                reauthentication_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, ContentHash(content), actor, f.Signing.Meaning, proof.Method, proof.AuthenticatedAt, proof.Session, proof.ID)
		if err := signatureRefusal(err); err != nil {
			return err
		}
		got, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		out = &got.Baseline
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineSigned, TargetType: "baseline", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": f.ProjectKey, "name": f.Name, "signatures": got.Signing.Signatures,
				"required": got.Signing.Required, "method": proof.Method}})
	})
	return out, lsn, err
}

// prove checks the signer's password before the transaction, as a review's decision does.
func (s *Service) prove(ctx context.Context, actor uuid.UUID, in SignInput) error {
	err := signing.Prove(ctx, s.passwords, actor, in.Password, in.Reauthentication,
		"Signing a baseline is an electronic signature. Sign with your password, or sign in again at the identity provider.")
	var field *signing.FieldError
	if errors.As(err, &field) {
		return &FieldError{field.Field, field.Message}
	}
	return err
}

// signatureRefusal names what the guard refused a signature for.
func signatureRefusal(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		if err != nil {
			return fmt.Errorf("record the signature: %w", err)
		}
		return nil
	}
	switch pgErr.ConstraintName {
	case "baseline_signature_unproven":
		return signing.ErrSpent
	case "baseline_signature_once":
		return ErrAlreadySigned
	case "baseline_signature_no_rule":
		return ErrNoSigningRule
	case "baseline_signature_not_theirs":
		return ErrNotSigner
	case "baseline_signature_wrong_content":
		return ErrChanged
	}
	return fmt.Errorf("record the signature: %w", err)
}

// Content is what a baseline's hash covers.
type Content struct {
	BaselineID, ProjectID uuid.UUID
	RootID                *uuid.UUID
	Items                 []ContentItem
	Links                 []ContentLink
	External, Pages       []uuid.UUID
	// Evidence is what the baseline recorded of each requirement's test results (#200), code tags (#201) and, for
	// a test case, its last result (#252).
	Evidence []ContentEvidence
	// Computed are the computed values it recorded (#213), each line as baseline_computed_line writes it.
	Computed []ContentEvidence
}

// ContentEvidence is one piece of evidence a baseline recorded, its line as baseline_evidence_line writes it.
type ContentEvidence struct {
	RequirementID uuid.UUID
	Line          string
}

// ContentItem is a requirement or section at the revision the baseline holds; Hash is empty for one purged since.
type ContentItem struct {
	RequirementID uuid.UUID
	Revision      int
	Hash          string
	Path          []int32
}

// ContentLink is a trace link the baseline holds.
type ContentLink struct {
	TypeID, SourceID, TargetID uuid.UUID
}

// ContentHash is the SHA-256, in lowercase hex, of a baseline's content in the form docs/api-contract-m8.md gives,
// which baseline_content_hash computes in the database for the guard.
func ContentHash(c Content) string {
	var b strings.Builder
	root := "-"
	if c.RootID != nil {
		root = c.RootID.String()
	}
	fmt.Fprintf(&b, "baseline %s %s %s\n", c.BaselineID, c.ProjectID, root)
	items := slices.Clone(c.Items)
	slices.SortFunc(items, func(x, y ContentItem) int {
		return cmp.Or(slices.Compare(x.Path, y.Path), strings.Compare(x.RequirementID.String(), y.RequirementID.String()))
	})
	for _, i := range items {
		hash := i.Hash
		if hash == "" {
			hash = "-"
		}
		parts := make([]string, len(i.Path))
		for n, p := range i.Path {
			parts[n] = strconv.Itoa(int(p))
		}
		fmt.Fprintf(&b, "item %s %d %s %s\n", i.RequirementID, i.Revision, hash, strings.Join(parts, "."))
	}
	links := slices.Clone(c.Links)
	slices.SortFunc(links, func(x, y ContentLink) int {
		return cmp.Or(strings.Compare(x.TypeID.String(), y.TypeID.String()), strings.Compare(x.SourceID.String(), y.SourceID.String()),
			strings.Compare(x.TargetID.String(), y.TargetID.String()))
	})
	for _, l := range links {
		fmt.Fprintf(&b, "link %s %s %s\n", l.TypeID, l.SourceID, l.TargetID)
	}
	for _, line := range []struct {
		kind string
		ids  []uuid.UUID
	}{{"external", c.External}, {"page", c.Pages}} {
		ids := slices.Clone(line.ids)
		slices.SortFunc(ids, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
		for _, id := range ids {
			fmt.Fprintf(&b, "%s %s\n", line.kind, id)
		}
	}
	evidence := slices.Clone(c.Evidence)
	slices.SortFunc(evidence, func(x, y ContentEvidence) int {
		return cmp.Or(strings.Compare(x.RequirementID.String(), y.RequirementID.String()), strings.Compare(x.Line, y.Line))
	})
	for _, e := range evidence {
		fmt.Fprintf(&b, "evidence %s %s\n", e.RequirementID, e.Line)
	}
	computed := slices.Clone(c.Computed)
	slices.SortFunc(computed, func(x, y ContentEvidence) int {
		return cmp.Or(strings.Compare(x.RequirementID.String(), y.RequirementID.String()), strings.Compare(x.Line, y.Line))
	})
	for _, v := range computed {
		fmt.Fprintf(&b, "%s %s\n", v.RequirementID, v.Line)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// readContent reads what a baseline's hash covers, as baseline_content_rows hands it to a reader of its project.
func readContent(ctx context.Context, tx db.DBTX, f *found) (Content, error) {
	c := Content{BaselineID: f.ID, ProjectID: f.projectID}
	if f.Root != nil {
		id := f.Root.ID
		c.RootID = &id
	}
	rows, err := tx.Query(ctx, `SELECT kind, a, b, c, revision, hash, path FROM baseline_content_rows($1)`, f.ID)
	if err != nil {
		return c, fmt.Errorf("read what the baseline holds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			kind     string
			a        uuid.UUID
			b, t     *uuid.UUID
			revision *int
			hash     *string
			path     []int32
		)
		if err := rows.Scan(&kind, &a, &b, &t, &revision, &hash, &path); err != nil {
			return c, err
		}
		switch kind {
		case "item":
			item := ContentItem{RequirementID: a, Revision: *revision, Path: path}
			if hash != nil {
				item.Hash = *hash
			}
			c.Items = append(c.Items, item)
		case "link":
			c.Links = append(c.Links, ContentLink{TypeID: a, SourceID: *b, TargetID: *t})
		case "external":
			c.External = append(c.External, a)
		case "page":
			c.Pages = append(c.Pages, a)
		case "evidence":
			c.Evidence = append(c.Evidence, ContentEvidence{RequirementID: a, Line: *hash})
		case "computed":
			c.Computed = append(c.Computed, ContentEvidence{RequirementID: a, Line: *hash})
		}
	}
	return c, rows.Err()
}

// takeSessionItems records the requirements a closing review session approved, at their revisions now, and the
// sections above them (#204); $1 is the project, $2 the baseline, $3 the requirements.
const takeSessionItems = requirement.WalkTree + `, kept AS (
	SELECT id, parent_id FROM requirement WHERE id = ANY($3::uuid[])
	UNION
	SELECT r.id, r.parent_id FROM requirement r JOIN kept k ON r.id = k.parent_id
)
INSERT INTO baseline_item (baseline_id, requirement_id, number, kind, revision, parent_id, rank, path, status_id, status_name,
                           status_category, type_id)
SELECT $2, r.id, r.number, r.kind, r.revision, r.parent_id, r.rank, w.path, r.status_id, st.name, st.category, r.type_id
FROM walk w JOIN requirement r ON r.id = w.id LEFT JOIN req_status st ON st.id = r.status_id
WHERE w.id IN (SELECT id FROM kept)`

// TakeOfSession takes, in a closing review session's transaction, a baseline of what it approved, which its
// moderator may take as review_session_closing lets them (#204).
func TakeOfSession(ctx context.Context, tx db.DBTX, actor, projectID, sessionID uuid.UUID, name, description string, approved []uuid.UUID) (uuid.UUID, error) {
	name, err := checkName(name)
	if err != nil {
		return uuid.Nil, err
	}
	if description, err = checkDescription(description); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO baseline (project_id, name, description, created_by, detail, session_id, computed_stale)
		VALUES ($1, $2, $3, $4, $5, $6, computed_pending($1)) RETURNING id`, projectID, name, description, actor, DetailFull, sessionID).Scan(&id)
	if err != nil {
		return uuid.Nil, nameRefusal(err)
	}
	for _, step := range []struct {
		sql  string
		args []any
	}{{takeSessionItems, []any{projectID, id, approved}}, {takeLinks, []any{id}}, {takeExternal, []any{id}}, {takePages, []any{id}},
		{takeEvidence, []any{id}}, {takeCode, []any{id}}, {takeRuns, []any{id}}, {takeComputed, []any{id}}} {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			return uuid.Nil, fmt.Errorf("take the session's baseline: %w", err)
		}
	}
	if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionBaselineTaken, TargetType: "baseline", TargetID: &id, Actor: actor,
		Data: map[string]any{"name": name, "session": sessionID, "items": len(approved)}}); err != nil {
		return uuid.Nil, err
	}
	return id, events.Emit(ctx, tx, TopicTaken, map[string]any{"actorId": actor, "baselineId": id, "projectId": projectID})
}
