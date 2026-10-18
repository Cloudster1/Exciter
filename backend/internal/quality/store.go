package quality

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/markdown"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// The events of the writing rules: a project's requirements to be checked again, and its rules set.
const (
	TopicRecheck       = project.TopicQualityRecheck
	TopicSettingsSaved = "quality_settings.saved"
)

var (
	// ErrForbidden is a change by somebody who may not make it: settings without administer, a finding without edit.
	ErrForbidden = errors.New("not allowed to change the writing rules here")
	// ErrStale is an acceptance against a revision that is no longer current.
	ErrStale = errors.New("the requirement changed since it was read")
)

// FieldError is a field that does not hold, with the sentence that says how to fix it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Located is a requirement as the rules need it, found by its key as the reader sees it.
type Located struct {
	ID, ProjectID uuid.UUID
	TypeID        *uuid.UUID
	Key           string
	Revision      int
	Section       bool
	// Editable is the reader editing it while it is out of the trash and its project open.
	Editable bool
	Archived bool
}

// Locator finds a requirement by its key, or a key it had; the requirement package answers it, which calls this one.
type Locator func(ctx context.Context, tx db.DBTX, reader uuid.UUID, key string) (*Located, error)

// Service checks drafts, reads and accepts findings, and keeps a project's rules.
type Service struct {
	db     *db.Cluster
	locate Locator
}

func NewService(cluster *db.Cluster, locate Locator) *Service {
	return &Service{db: cluster, locate: locate}
}

// setup is what checking a project's requirements depends on beyond their words.
type setup struct {
	lang   Language
	words  map[RuleID]Words
	avoid  []AvoidTerm
	config string
}

// setupOf reads a project's language and word lists.
func setupOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*setup, error) {
	out := &setup{words: map[RuleID]Words{}}
	var lang string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(settings->>'language', '') FROM project WHERE id = $1`, projectID).Scan(&lang); err != nil {
		return nil, fmt.Errorf("read the project's language: %w", err)
	}
	out.lang = Language(project.Settings{Language: lang}.LanguageOf())
	rows, err := tx.Query(ctx, `
		SELECT rule, add_words, except_words FROM quality_rule_setting
		WHERE project_id = $1 AND (cardinality(add_words) > 0 OR cardinality(except_words) > 0)`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the word lists: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rule string
		var w Words
		if err := rows.Scan(&rule, &w.Add, &w.Except); err != nil {
			return nil, err
		}
		out.words[RuleID(rule)] = w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.config = ConfigHash(out.lang, out.words, out.avoid)
	return out, nil
}

// Record checks a requirement's revision and keeps what the rules found, in the transaction that wrote the revision,
// carrying forward what an author accepted in the revision before.
func Record(ctx context.Context, tx db.DBTX, requirementID uuid.UUID, revision int) error {
	return record(ctx, tx, requirementID, revision, nil, false)
}

func record(ctx context.Context, tx db.DBTX, id uuid.UUID, revision int, s *setup, again bool) error {
	var (
		kind, title string
		projectID   uuid.UUID
		statement   json.RawMessage
	)
	err := tx.QueryRow(ctx, `
		SELECT r.kind, r.project_id, v.title, v.statement FROM requirement r
		JOIN requirement_revision v ON v.requirement_id = r.id AND v.number = $2
		WHERE r.id = $1`, id, revision).Scan(&kind, &projectID, &title, &statement)
	if err != nil {
		return fmt.Errorf("read revision %d to check it: %w", revision, err)
	}
	// A section says nothing that shall be, so there is nothing to score.
	if kind != "requirement" {
		return nil
	}
	if s == nil {
		if s, err = setupOf(ctx, tx, projectID); err != nil {
			return err
		}
	}
	result := Examine(Input{Language: s.lang, Title: title, Statement: parsed(statement), Words: s.words, Avoid: s.avoid})
	if again {
		if _, err := tx.Exec(ctx, `DELETE FROM quality_finding WHERE requirement_id = $1 AND revision = $2`, id, revision); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO requirement_quality (requirement_id, revision, config) VALUES ($1, $2, $3)
			ON CONFLICT (requirement_id, revision) DO UPDATE SET config = EXCLUDED.config, checked_at = now()`, id, revision, s.config); err != nil {
			return fmt.Errorf("note the check: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `INSERT INTO requirement_quality (requirement_id, revision, config) VALUES ($1, $2, $3)`,
		id, revision, s.config); err != nil {
		return fmt.Errorf("note the check: %w", err)
	}
	if err := writeHits(ctx, tx, id, revision, result.Hits); err != nil {
		return err
	}
	if again || revision < 2 || len(result.Hits) == 0 {
		return nil
	}
	keys := make([]string, len(result.Hits))
	for i, h := range result.Hits {
		keys[i] = h.Key
	}
	// What the author accepted stays while the same words are found again.
	if _, err := tx.Exec(ctx, `
		INSERT INTO quality_acceptance (requirement_id, revision, key, reason, carried_from, accepted_by, accepted_at)
		SELECT a.requirement_id, $2, a.key, a.reason, COALESCE(a.carried_from, a.revision), a.accepted_by, a.accepted_at
		FROM quality_acceptance a WHERE a.requirement_id = $1 AND a.revision = $2 - 1 AND a.key = ANY ($3)`, id, revision, keys); err != nil {
		return fmt.Errorf("carry the accepted findings forward: %w", err)
	}
	return nil
}

func writeHits(ctx context.Context, tx db.DBTX, id uuid.UUID, revision int, hits []Hit) error {
	if len(hits) == 0 {
		return nil
	}
	// A hostile statement finds without end; what is past the limit would only repeat the lesson.
	hits = hits[:min(len(hits), MaxFindings)]
	var (
		keys, rules, fields, matches []string
		blocks, froms, tos           []int
	)
	for _, h := range hits {
		keys, rules, fields = append(keys, h.Key), append(rules, string(h.Rule)), append(fields, string(h.Field))
		blocks, froms, tos = append(blocks, h.Block), append(froms, h.From), append(tos, h.To)
		matches = append(matches, cut(h.Match, MaxMatchChars))
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO quality_finding (requirement_id, revision, key, rule, field, block, start_at, end_at, match)
		SELECT $1, $2, k, r, f, b, s, e, m
		FROM unnest($3::text[], $4::text[], $5::text[], $6::int[], $7::int[], $8::int[], $9::text[]) AS x(k, r, f, b, s, e, m)`,
		id, revision, keys, rules, fields, blocks, froms, tos, matches); err != nil {
		return fmt.Errorf("keep the findings: %w", err)
	}
	return nil
}

// parsed reads a stored statement; one that does not read is checked as empty, as it was never saved so.
func parsed(raw json.RawMessage) document.Node {
	var n document.Node
	if err := json.Unmarshal(raw, &n); err != nil {
		return document.Node{Type: "doc"}
	}
	return n
}

func cut(s string, chars int) string {
	if utf8.RuneCountInString(s) <= chars {
		return s
	}
	return string([]rune(s)[:chars])
}

// state is how a project uses a rule for one type.
type state struct {
	enabled  bool
	severity Severity
}

func statesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID, typeID *uuid.UUID) (map[RuleID]state, error) {
	rows, err := tx.Query(ctx, `SELECT rule, enabled, severity FROM quality_rule_state($1, $2)`, projectID, typeID)
	if err != nil {
		return nil, fmt.Errorf("read the rules: %w", err)
	}
	defer rows.Close()
	out := map[RuleID]state{}
	for rows.Next() {
		var rule, severity string
		var st state
		if err := rows.Scan(&rule, &st.enabled, &severity); err != nil {
			return nil, err
		}
		st.severity = Severity(severity)
		out[RuleID(rule)] = st
	}
	return out, rows.Err()
}

// acceptancesOf are what authors accepted in one revision, by finding key.
func acceptancesOf(ctx context.Context, tx db.DBTX, id uuid.UUID, revision int) (map[string]*Acceptance, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.key, a.reason, a.revision, a.carried_from, a.accepted_at, COALESCE(u.id, '00000000-0000-0000-0000-000000000000'), COALESCE(u.name, '')
		FROM quality_acceptance a LEFT JOIN app_user u ON u.id = a.accepted_by
		WHERE a.requirement_id = $1 AND a.revision = $2`, id, revision)
	if err != nil {
		return nil, fmt.Errorf("read the accepted findings: %w", err)
	}
	defer rows.Close()
	out := map[string]*Acceptance{}
	for rows.Next() {
		var key string
		a := &Acceptance{}
		if err := rows.Scan(&key, &a.Reason, &a.Revision, &a.CarriedFrom, &a.At, &a.By.ID, &a.By.Name); err != nil {
			return nil, err
		}
		out[key] = a
	}
	return out, rows.Err()
}

// judge gives each hit the project's verdict, and scores what counts.
func judge(hits []Hit, states map[RuleID]state, accepted map[string]*Acceptance) ([]Finding, int) {
	out := make([]Finding, 0, len(hits))
	judged := make([]Judged, 0, len(hits))
	for _, h := range hits {
		st, ok := states[h.Rule]
		if !ok {
			info, _ := RuleOf(h.Rule)
			st = state{enabled: info.DefaultEnabled, severity: info.DefaultSeverity}
		}
		message, fix := Explain(h.Rule, h.Match)
		f := Finding{Key: h.Key, Rule: h.Rule, Severity: st.severity, Span: h.Span, Match: h.Match, Message: message, Fix: fix,
			Accepted: accepted[h.Key]}
		f.Active = st.enabled && f.Accepted == nil
		out = append(out, f)
		judged = append(judged, Judged{Rule: h.Rule, Severity: st.severity, Active: f.Active})
	}
	return out, Score(judged)
}

// CheckDraft is what the rules would find in a draft, as the project would judge it, without writing anything.
func (s *Service) CheckDraft(ctx context.Context, reader uuid.UUID, projectKey string, in CheckInput) (*Check, error) {
	var out *Check
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return ErrForbidden
		}
		statement, err := draftStatement(ctx, tx, in)
		if err != nil {
			return err
		}
		typeID, accepted := in.TypeID, map[string]*Acceptance{}
		if key := strings.TrimSpace(in.RequirementKey); key != "" {
			r, err := s.locate(ctx, tx, reader, key)
			if err != nil {
				return err
			}
			if r.ProjectID != p.ID {
				return &FieldError{"requirementKey", "Name a requirement of this project."}
			}
			if typeID == nil {
				typeID = r.TypeID
			}
			if accepted, err = acceptancesOf(ctx, tx, r.ID, r.Revision); err != nil {
				return err
			}
		}
		if typeID == nil {
			typeID = p.Settings.DefaultTypeID
		}
		set, err := setupOf(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		states, err := statesOf(ctx, tx, p.ID, typeID)
		if err != nil {
			return err
		}
		result := Examine(Input{Language: set.lang, Title: in.Title, Statement: statement, Words: set.words, Avoid: set.avoid})
		findings, score := judge(result.Hits, states, accepted)
		out = &Check{Score: &score, Language: set.lang, Findings: findings, Sentences: orEmpty(result.Sentences), Terms: []TermMatch{}}
		return nil
	})
	return out, err
}

// draftStatement reads a draft's statement, as a document or as Markdown.
func draftStatement(ctx context.Context, tx db.DBTX, in CheckInput) (document.Node, error) {
	if in.StatementMarkdown != nil {
		n, err := markdown.Convert(*in.StatementMarkdown, markdown.Resolve{Person: markdown.Members(ctx, tx)})
		if err != nil {
			var refusal *markdown.Refusal
			if errors.As(err, &refusal) {
				return document.Node{}, &FieldError{"statementMarkdown", refusal.Error()}
			}
			return document.Node{}, err
		}
		return n, nil
	}
	if len(in.Statement) == 0 || string(in.Statement) == "null" {
		return document.Node{Type: "doc"}, nil
	}
	n, err := document.Parse(in.Statement)
	if err != nil {
		var invalid *document.InvalidError
		if errors.As(err, &invalid) {
			return document.Node{}, &FieldError{"statement", invalid.Message}
		}
		return document.Node{}, err
	}
	return n, nil
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Findings are what the rules found in a revision of a requirement, the current one when revision is nil.
func (s *Service) Findings(ctx context.Context, reader uuid.UUID, key string, revision *int) (*Findings, error) {
	var out *Findings
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := s.locate(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		out, err = findingsOf(ctx, tx, r, revision)
		return err
	})
	return out, err
}

func findingsOf(ctx context.Context, tx db.DBTX, r *Located, revision *int) (*Findings, error) {
	n := r.Revision
	if revision != nil {
		n = *revision
	}
	if n < 1 || n > r.Revision {
		return nil, &FieldError{"revision", fmt.Sprintf("%s has revisions 1 to %d. Name one of them.", r.Key, r.Revision)}
	}
	out := &Findings{Revision: n, Findings: []Finding{}, Sentences: []Sentence{}}
	if r.Section {
		return out, nil
	}
	var (
		checked     bool
		title       string
		statement   json.RawMessage
		typeID      *uuid.UUID
		staleConfig bool
	)
	if err := tx.QueryRow(ctx, `
		SELECT v.title, v.statement, v.type_id, c.requirement_id IS NOT NULL,
		       c.requirement_id IS NOT NULL AND pq.config IS NOT NULL AND c.config <> pq.config
		FROM requirement_revision v
		LEFT JOIN requirement_quality c ON c.requirement_id = v.requirement_id AND c.revision = v.number
		LEFT JOIN project_quality pq ON pq.project_id = $3
		WHERE v.requirement_id = $1 AND v.number = $2`, r.ID, n, r.ProjectID).Scan(&title, &statement, &typeID, &checked, &staleConfig); err != nil {
		return nil, fmt.Errorf("read revision %d: %w", n, err)
	}
	if n == r.Revision {
		typeID = r.TypeID
	}
	out.Stale = n == r.Revision && (!checked || staleConfig)
	set, err := setupOf(ctx, tx, r.ProjectID)
	if err != nil {
		return nil, err
	}
	// The shapes of sentences are not kept; they follow from the words alone.
	out.Sentences = orEmpty(Examine(Input{Language: set.lang, Title: title, Statement: parsed(statement)}).Sentences)
	if !checked {
		return out, nil
	}
	hits, err := hitsOf(ctx, tx, r.ID, n)
	if err != nil {
		return nil, err
	}
	states, err := statesOf(ctx, tx, r.ProjectID, typeID)
	if err != nil {
		return nil, err
	}
	accepted, err := acceptancesOf(ctx, tx, r.ID, n)
	if err != nil {
		return nil, err
	}
	findings, score := judge(hits, states, accepted)
	out.Findings, out.Score = findings, &score
	for _, f := range findings {
		switch {
		case f.Accepted != nil:
			out.Accepted++
		case f.Active:
			out.Active++
		}
	}
	return out, nil
}

// hitsOf are the findings kept for a revision, title first and in the order of the text.
func hitsOf(ctx context.Context, tx db.DBTX, id uuid.UUID, revision int) ([]Hit, error) {
	rows, err := tx.Query(ctx, `
		SELECT key, rule, field, block, start_at, end_at, match FROM quality_finding
		WHERE requirement_id = $1 AND revision = $2`, id, revision)
	if err != nil {
		return nil, fmt.Errorf("read the findings: %w", err)
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		var rule, field string
		if err := rows.Scan(&h.Key, &rule, &field, &h.Block, &h.From, &h.To, &h.Match); err != nil {
			return nil, err
		}
		h.Rule, h.Field = RuleID(rule), Field(field)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(fieldOrder(a.Field), fieldOrder(b.Field)), cmp.Compare(a.Block, b.Block),
			cmp.Compare(a.From, b.From), cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Key, b.Key))
	})
	return out, nil
}

func fieldOrder(f Field) int {
	if f == FieldTitle {
		return 0
	}
	return 1
}

// Accept accepts a finding of the current revision with a reason, in the actor's name.
func (s *Service) Accept(ctx context.Context, actor uuid.UUID, key string, in AcceptInput) (*Findings, db.LSN, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > MaxReasonChars {
		return nil, 0, &FieldError{"reason", fmt.Sprintf("Say in at most %d characters why the finding stays.", MaxReasonChars)}
	}
	return s.onFinding(ctx, actor, key, in.Revision, in.Key, func(ctx context.Context, tx db.DBTX, r *Located) error {
		if _, err := tx.Exec(ctx, `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, accepted_by) VALUES ($1, $2, $3, $4, $5)`,
			r.ID, r.Revision, in.Key, reason, actor); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return &FieldError{"key", "That finding is accepted already."}
			}
			return fmt.Errorf("accept the finding: %w", err)
		}
		return nil
	})
}

// Withdraw takes back the acceptance of a finding of the current revision.
func (s *Service) Withdraw(ctx context.Context, actor uuid.UUID, key string, revision int, findingKey string) (*Findings, db.LSN, error) {
	return s.onFinding(ctx, actor, key, revision, findingKey, func(ctx context.Context, tx db.DBTX, r *Located) error {
		tag, err := tx.Exec(ctx, `DELETE FROM quality_acceptance WHERE requirement_id = $1 AND revision = $2 AND key = $3`, r.ID, r.Revision, findingKey)
		if err != nil {
			return fmt.Errorf("take the acceptance back: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return &FieldError{"key", "That finding is not accepted. Read the findings again."}
		}
		return nil
	})
}

func (s *Service) onFinding(ctx context.Context, actor uuid.UUID, key string, revision int, findingKey string,
	change func(context.Context, db.DBTX, *Located) error) (*Findings, db.LSN, error) {
	var out *Findings
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := s.locate(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		switch {
		case r.Archived:
			return project.ErrArchived
		case !r.Editable:
			return ErrForbidden
		case revision != r.Revision:
			return ErrStale
		}
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM quality_finding WHERE requirement_id = $1 AND revision = $2 AND key = $3)`,
			r.ID, r.Revision, findingKey).Scan(&found); err != nil {
			return err
		}
		if !found {
			return &FieldError{"key", "The current revision has no such finding. Read the findings again."}
		}
		if err := change(ctx, tx, r); err != nil {
			return err
		}
		out, err = findingsOf(ctx, tx, r, nil)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// RulesCatalog is every rule there is.
func (s *Service) RulesCatalog() []RuleInfo { return slices.Clone(Rules) }

// Settings are a project's rules and word lists, each rule as it starts out where the project says nothing.
func (s *Service) Settings(ctx context.Context, reader uuid.UUID, projectKey string) (*Settings, error) {
	var out *Settings
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = settingsOf(ctx, tx, p)
		return err
	})
	return out, err
}

func settingsOf(ctx context.Context, tx db.DBTX, p *project.Project) (*Settings, error) {
	type row struct {
		enabled  bool
		severity string
		types    []uuid.UUID
		add      []string
		except   []string
	}
	rows, err := tx.Query(ctx, `
		SELECT rule, enabled, severity, type_ids, add_words, except_words, updated_at FROM quality_rule_setting WHERE project_id = $1`, p.ID)
	if err != nil {
		return nil, fmt.Errorf("read the rules: %w", err)
	}
	defer rows.Close()
	set := map[RuleID]row{}
	var updated *time.Time
	for rows.Next() {
		var rule string
		var r row
		var at time.Time
		if err := rows.Scan(&rule, &r.enabled, &r.severity, &r.types, &r.add, &r.except, &at); err != nil {
			return nil, err
		}
		set[RuleID(rule)] = r
		if updated == nil || at.After(*updated) {
			updated = &at
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := &Settings{Language: Language(p.Settings.LanguageOf()), UpdatedAt: updated,
		SettingsInput: SettingsInput{Rules: []RuleSetting{}, Words: map[RuleID]Words{}}}
	for _, info := range Rules {
		rs := RuleSetting{Rule: info.ID, Enabled: info.DefaultEnabled, Severity: info.DefaultSeverity, TypeIDs: []uuid.UUID{}}
		if r, ok := set[info.ID]; ok {
			rs.Enabled, rs.Severity, rs.TypeIDs = r.enabled, Severity(r.severity), orEmpty(r.types)
		}
		out.Rules = append(out.Rules, rs)
		if info.Words {
			w := Words{Add: []string{}, Except: []string{}}
			if r, ok := set[info.ID]; ok {
				w.Add, w.Except = orEmpty(r.add), orEmpty(r.except)
			}
			out.Words[info.ID] = w
		}
	}
	return out, nil
}

// SaveSettings sets every rule and word list at once; a changed list has the worker check the requirements again.
func (s *Service) SaveSettings(ctx context.Context, actor uuid.UUID, projectKey string, in SettingsInput) (*Settings, db.LSN, error) {
	rules, words, err := checkSettings(in)
	if err != nil {
		return nil, 0, err
	}
	var out *Settings
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		switch {
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Administer:
			return ErrForbidden
		}
		before, err := setupOf(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if err := checkTypes(ctx, tx, p.ID, rules); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM quality_rule_setting WHERE project_id = $1`, p.ID); err != nil {
			return err
		}
		for _, r := range rules {
			w := words[r.Rule]
			if _, err := tx.Exec(ctx, `
				INSERT INTO quality_rule_setting (project_id, rule, enabled, severity, type_ids, add_words, except_words, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				p.ID, string(r.Rule), r.Enabled, string(r.Severity), r.TypeIDs, orEmpty(w.Add), orEmpty(w.Except), actor); err != nil {
				return fmt.Errorf("set the rule %s: %w", r.Rule, err)
			}
		}
		after, err := setupOf(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionQualitySettingsSaved, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"rules": rules, "words": words}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicSettingsSaved, map[string]any{"actorId": actor, "projectId": p.ID}); err != nil {
			return err
		}
		if after.config != before.config {
			if err := events.Emit(ctx, tx, TopicRecheck, map[string]any{"actorId": actor, "projectId": p.ID}); err != nil {
				return err
			}
		}
		out, err = settingsOf(ctx, tx, p)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// checkSettings holds settings to the rules there are: every rule once, a severity it knows, words for the rules
// that take them, each list trimmed, without repeats and within its limits. A rule left out takes its defaults.
func checkSettings(in SettingsInput) ([]RuleSetting, map[RuleID]Words, error) {
	given := map[RuleID]RuleSetting{}
	for i, r := range in.Rules {
		field := fmt.Sprintf("rules[%d]", i)
		if _, ok := RuleOf(r.Rule); !ok {
			return nil, nil, &FieldError{field + ".rule", "That is not a writing rule. Use one GET /quality-rules lists."}
		}
		if _, twice := given[r.Rule]; twice {
			return nil, nil, &FieldError{field + ".rule", "Name each rule once."}
		}
		if !slices.Contains(Severities, r.Severity) {
			return nil, nil, &FieldError{field + ".severity", "Choose error, warning or hint."}
		}
		r.TypeIDs = orEmpty(r.TypeIDs)
		given[r.Rule] = r
	}
	words := map[RuleID]Words{}
	for rule, w := range in.Words {
		info, ok := RuleOf(rule)
		if !ok || !info.Words {
			return nil, nil, &FieldError{"words." + string(rule), "Only vague, unbounded, escape and placeholder take words of the project's own."}
		}
		add, err := checkWords("words."+string(rule)+".add", w.Add)
		if err != nil {
			return nil, nil, err
		}
		except, err := checkWords("words."+string(rule)+".except", w.Except)
		if err != nil {
			return nil, nil, err
		}
		words[rule] = Words{Add: add, Except: except}
	}
	out := make([]RuleSetting, 0, len(Rules))
	for _, info := range Rules {
		r, ok := given[info.ID]
		if !ok {
			w, listed := words[info.ID]
			if !listed || (len(w.Add) == 0 && len(w.Except) == 0) {
				continue
			}
			r = RuleSetting{Rule: info.ID, Enabled: info.DefaultEnabled, Severity: info.DefaultSeverity, TypeIDs: []uuid.UUID{}}
		}
		out = append(out, r)
	}
	return out, words, nil
}

func checkWords(field string, in []string) ([]string, error) {
	out, seen := []string{}, map[string]bool{}
	for _, w := range in {
		w = strings.Join(strings.Fields(w), " ")
		if w == "" || seen[strings.ToLower(w)] {
			continue
		}
		if utf8.RuneCountInString(w) > MaxWordLength {
			return nil, &FieldError{field, fmt.Sprintf("Keep each word or phrase to at most %d characters.", MaxWordLength)}
		}
		seen[strings.ToLower(w)] = true
		out = append(out, w)
	}
	if len(out) > MaxCustomWords {
		return nil, &FieldError{field, fmt.Sprintf("A list holds at most %d words.", MaxCustomWords)}
	}
	return out, nil
}

// checkTypes holds the types a rule applies to to the project's.
func checkTypes(ctx context.Context, tx db.DBTX, projectID uuid.UUID, rules []RuleSetting) error {
	for i, r := range rules {
		if len(r.TypeIDs) == 0 {
			continue
		}
		var known int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM requirement_type WHERE project_id = $1 AND id = ANY ($2)`, projectID, r.TypeIDs).Scan(&known); err != nil {
			return err
		}
		distinct := map[uuid.UUID]bool{}
		for _, id := range r.TypeIDs {
			distinct[id] = true
		}
		if known != len(r.TypeIDs) || len(distinct) != len(r.TypeIDs) {
			return &FieldError{fmt.Sprintf("rules[%d].typeIds", i), "Name types of this project, each once."}
		}
	}
	return nil
}

// Recheck checks a project's requirements again where the words they were checked with differ from today's, as the
// admin role and a batch at a time, so a long project neither holds one transaction nor starts over.
func (s *Service) Recheck(ctx context.Context, projectID uuid.UUID) error {
	var after uuid.UUID
	for {
		var done bool
		if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			set, err := setupOf(ctx, tx, projectID)
			if errors.Is(err, pgx.ErrNoRows) {
				done = true
				return nil
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO project_quality (project_id, config) VALUES ($1, $2)
				ON CONFLICT (project_id) DO UPDATE SET config = EXCLUDED.config, changed_at = now()
				WHERE project_quality.config <> EXCLUDED.config`, projectID, set.config); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT r.id, r.revision FROM requirement r
				LEFT JOIN requirement_quality c ON c.requirement_id = r.id AND c.revision = r.revision
				WHERE r.project_id = $1 AND r.kind = 'requirement' AND r.id > $2 AND c.config IS DISTINCT FROM $3
				ORDER BY r.id LIMIT $4`, projectID, after, set.config, RecheckBatch)
			if err != nil {
				return err
			}
			type due struct {
				id       uuid.UUID
				revision int
			}
			batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
				var d due
				return d, row.Scan(&d.id, &d.revision)
			})
			if err != nil {
				return err
			}
			for _, d := range batch {
				if err := record(ctx, tx, d.id, d.revision, set, true); err != nil {
					return err
				}
				after = d.id
			}
			done = len(batch) < RecheckBatch
			return nil
		}); err != nil {
			return fmt.Errorf("check the requirements again: %w", err)
		}
		if done {
			return nil
		}
	}
}

// Handler checks a project's requirements again for the worker.
func Handler(s *Service) events.HandlerFunc {
	return func(ctx context.Context, e events.Event) error {
		var p struct {
			ProjectID uuid.UUID `json:"projectId"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("read %s: %w", e.Topic, err)
		}
		return s.Recheck(ctx, p.ProjectID)
	}
}
