package codetrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// The events of code scans: a scan uploaded, which judges coverage again and tells owners of what it brought, and
// the tag words set.
const (
	TopicUploaded   = "code_scan.uploaded"
	TopicWordsSaved = "code_tag_words.saved"
)

var (
	// ErrNotFound is also the answer for a scan the caller may not read.
	ErrNotFound = errors.New("code scan not found")
	// ErrForbidden is an upload to a project the caller does not edit, or tag words they do not keep.
	ErrForbidden = errors.New("the project's editors upload code scans")
	// ErrTooManyTags is a scan of more than MaxScanTags tags.
	ErrTooManyTags = errors.New("too many tags")
)

// FieldError is a field that does not hold, with the sentence that says how to fix it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

var wordShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// Service keeps the tags scans of a project's repositories found.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// wordsOf are a project's tag words, the defaults without a row of its own.
func wordsOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (Words, *time.Time, error) {
	out := DefaultWords
	var at *time.Time
	err := tx.QueryRow(ctx, `SELECT implements, verifies, updated_at FROM code_tag_words WHERE project_id = $1`, projectID).
		Scan(&out.Implements, &out.Verifies, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, at, err
}

// relationOf is what a word means by the words given, empty for one they do not hold.
func relationOf(w Words, word string) Relation {
	match := func(list []string) bool {
		return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(x, word) })
	}
	switch {
	case match(w.Implements):
		return Implements
	case match(w.Verifies):
		return Verifies
	}
	return ""
}

// Words are a project's tag words and the keys its tags may carry, for the scanner and the settings.
func (s *Service) Words(ctx context.Context, reader uuid.UUID, projectKey string) (*TagWords, error) {
	var out TagWords
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		return tagWords(ctx, tx, p, &out)
	})
	return &out, err
}

func tagWords(ctx context.Context, tx db.DBTX, p *project.Project, out *TagWords) error {
	words, at, err := wordsOf(ctx, tx, p.ID)
	if err != nil {
		return err
	}
	out.Words, out.UpdatedAt = words, at
	out.ProjectKeys = []string{p.Key}
	rows, err := tx.Query(ctx, `SELECT key FROM project_key_alias WHERE project_id = $1 ORDER BY key`, p.ID)
	if err != nil {
		return fmt.Errorf("read the project's former keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return err
		}
		out.ProjectKeys = append(out.ProjectKeys, k)
	}
	return rows.Err()
}

// checkWords trims a project's tag words and says what does not hold.
func checkWords(in Words) (Words, error) {
	var out Words
	seen := map[string]Relation{}
	for _, r := range []struct {
		field    string
		relation Relation
		in       []string
		into     *[]string
	}{{"implements", Implements, in.Implements, &out.Implements}, {"verifies", Verifies, in.Verifies, &out.Verifies}} {
		for _, w := range r.in {
			w = strings.TrimSpace(w)
			if w == "" {
				continue
			}
			if !wordShape.MatchString(w) || utf8.RuneCountInString(w) > MaxWordLength {
				return out, &FieldError{r.field, fmt.Sprintf("%q is not a tag word. Start it with a letter and use letters, digits, - and _, at most %d.", w, MaxWordLength)}
			}
			if other, ok := seen[strings.ToLower(w)]; ok && other != r.relation {
				return out, &FieldError{r.field, fmt.Sprintf("%q means both implements and verifies. Give each relation words of its own.", w)}
			}
			if _, ok := seen[strings.ToLower(w)]; !ok {
				seen[strings.ToLower(w)] = r.relation
				*r.into = append(*r.into, w)
			}
		}
		if len(*r.into) == 0 || len(*r.into) > MaxWords {
			return out, &FieldError{r.field, fmt.Sprintf("Give %s one to %d words.", r.field, MaxWords)}
		}
	}
	return out, nil
}

// SaveWords sets the words a project's tags use; its administrators'. Tags are read by the words of the day.
func (s *Service) SaveWords(ctx context.Context, actor uuid.UUID, projectKey string, in Words) (*TagWords, db.LSN, error) {
	words, err := checkWords(in)
	if err != nil {
		return nil, 0, err
	}
	var out TagWords
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO code_tag_words (project_id, implements, verifies, updated_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (project_id) DO UPDATE SET implements = EXCLUDED.implements, verifies = EXCLUDED.verifies,
			    updated_by = EXCLUDED.updated_by, updated_at = now()`, p.ID, words.Implements, words.Verifies, actor); err != nil {
			return fmt.Errorf("set the tag words: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionCodeTagWordsSaved, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"implements": words.Implements, "verifies": words.Verifies}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicWordsSaved, map[string]any{"actorId": actor, "projectId": p.ID}); err != nil {
			return err
		}
		return tagWords(ctx, tx, p, &out)
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// checkScan trims a scan's fields and says which does not hold.
func checkScan(in *ScanInput) error {
	form := testresult.RunForm{Repository: in.Repository, Commit: in.Commit, Branch: in.Branch}
	if err := testresult.CheckForm(&form); err != nil {
		var field *testresult.FieldError
		if errors.As(err, &field) {
			return &FieldError{field.Field, field.Message}
		}
		return err
	}
	in.Repository, in.Commit, in.Branch, in.FileURL = form.Repository, form.Commit, form.Branch, strings.TrimSpace(in.FileURL)
	if in.FileURL != "" && (!testresult.WebAddress(in.FileURL) || !strings.Contains(in.FileURL, "{path}")) {
		return &FieldError{"fileUrl", "Give the address of a line as an http or https link with {commit}, {path} and {line} in it."}
	}
	if len(in.Tags) > MaxScanTags {
		return ErrTooManyTags
	}
	for i, t := range in.Tags {
		field := "tags[" + strconv.Itoa(i) + "]"
		switch {
		case strings.TrimSpace(t.Path) == "" || utf8.RuneCountInString(t.Path) > MaxPathLength:
			return &FieldError{field + ".path", "Give the file's path in the repository, in at most 1000 characters."}
		case t.Line < 1:
			return &FieldError{field + ".line", "Give the line the tag is on, counted from 1."}
		case t.Word == "" || utf8.RuneCountInString(t.Word) > MaxWordLength:
			return &FieldError{field + ".word", "Give the tag's word as the source writes it."}
		}
		if _, _, ok := requirement.ParseKey(t.Key); !ok {
			return &FieldError{field + ".key", fmt.Sprintf("%q is not a requirement key. Send keys such as CHK-R42.", t.Key)}
		}
	}
	return nil
}

// target is what a key reached, as the uploader sees it.
type target struct {
	id       uuid.UUID
	revision int
	trashed  bool
	section  bool
}

// before is a tag of the scan a new one replaces: its revision, and whether it was outdated when it was uploaded.
type before struct {
	revision int
	outdated bool
}

// placed names a tag regardless of its line, so a tag that moves keeps the revision it carried.
func placed(path, word, key string) string {
	return path + "\x00" + strings.ToLower(word) + "\x00" + key
}

// uploaded is what a scan's event carries: the requirements whose owners hear of it, and the unknown keys new to it.
type uploaded struct {
	ActorID  uuid.UUID   `json:"actorId"`
	ScanID   uuid.UUID   `json:"scanId"`
	Project  uuid.UUID   `json:"projectId"`
	Outdated []uuid.UUID `json:"outdated"`
	Orphaned []uuid.UUID `json:"orphaned"`
	Unknown  int         `json:"unknown"`
}

// Upload keeps a scan of a branch in place of the one before, each tag stamped with the revision it names or carries
// from that scan, and tells owners of what turned outdated or orphaned on the counting branch.
func (s *Service) Upload(ctx context.Context, actor uuid.UUID, projectKey string, in ScanInput) (*Scan, db.LSN, error) {
	if err := checkScan(&in); err != nil {
		return nil, 0, err
	}
	var out *Scan
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		switch {
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Edit:
			return ErrForbidden
		}
		words, _, err := wordsOf(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		var counting string
		if err := tx.QueryRow(ctx, `SELECT evidence_counting_branch($1)`, p.ID).Scan(&counting); err != nil {
			return err
		}
		earlier, err := previous(ctx, tx, p.ID, in.Repository, in.Branch)
		if err != nil {
			return err
		}
		targets, err := resolve(ctx, tx, p.ID, in.Tags)
		if err != nil {
			return err
		}
		id, ev, err := keep(ctx, tx, p.ID, in, words, targets, earlier)
		if err != nil {
			return err
		}
		ev.ActorID, ev.ScanID, ev.Project = actor, id, p.ID
		if in.Branch != counting {
			ev.Outdated, ev.Orphaned, ev.Unknown = nil, nil, 0
		}
		if err := events.Emit(ctx, tx, TopicUploaded, ev); err != nil {
			return err
		}
		out, err = findScan(ctx, tx, id)
		return err
	})
	return out, lsn, err
}

// previous reads the tags of the scan an upload replaces, by place, each outdated or not as it stood at its upload.
func previous(ctx context.Context, tx db.DBTX, projectID uuid.UUID, repository, branch string) (map[string]before, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.path, t.word, t.key, t.revision,
		       t.requirement_id IS NOT NULL AND t.revision < COALESCE((SELECT max(v.number) FROM requirement_revision v
		                                                               WHERE v.requirement_id = t.requirement_id AND v.created_at <= s.uploaded_at), 0)
		FROM code_scan s JOIN code_tag t ON t.scan_id = s.id
		WHERE s.project_id = $1 AND s.repository = $2 AND s.branch = $3 AND s.replaced_at IS NULL`, projectID, repository, branch)
	if err != nil {
		return nil, fmt.Errorf("read the scan before: %w", err)
	}
	defer rows.Close()
	out := map[string]before{}
	for rows.Next() {
		var (
			path, word, key string
			b               before
		)
		if err := rows.Scan(&path, &word, &key, &b.revision, &b.outdated); err != nil {
			return nil, err
		}
		out[placed(path, word, key)] = b
	}
	return out, rows.Err()
}

// resolve reads what each key reaches in the scan's project, a former key of it included; a key elsewhere is unknown.
func resolve(ctx context.Context, tx db.DBTX, projectID uuid.UUID, tags []FoundTag) (map[string]target, error) {
	var keys, projects []string
	var numbers []int
	for _, t := range tags {
		k := strings.ToUpper(strings.TrimSpace(t.Key))
		if slices.Contains(keys, k) {
			continue
		}
		pk, n, _ := requirement.ParseKey(k)
		keys, projects, numbers = append(keys, k), append(projects, pk), append(numbers, n)
	}
	out := map[string]target{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT x.k, r.id, r.revision, r.trashed_at IS NOT NULL, r.kind <> 'requirement'
		FROM unnest($1::text[], $2::text[], $3::int[]) AS x (k, p, n)
		JOIN requirement r ON r.id = requirement_key_id(x.p, x.n) AND r.project_id = $4`, keys, projects, numbers, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the keys the scan names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			key string
			t   target
		)
		if err := rows.Scan(&key, &t.id, &t.revision, &t.trashed, &t.section); err != nil {
			return nil, err
		}
		out[key] = t
	}
	return out, rows.Err()
}

// keep writes the scan in place of the one before and its tags, and answers what its event tells.
func keep(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in ScanInput, words Words, targets map[string]target,
	earlier map[string]before) (uuid.UUID, uploaded, error) {
	var (
		id                    uuid.UUID
		ev                    = uploaded{Outdated: []uuid.UUID{}, Orphaned: []uuid.UUID{}}
		reported              = []Reported{}
		unreported, linked    int
		paths, tagWords, keys []string
		lines, revisions      []int
		reqs                  []*uuid.UUID
		seen                  = map[string]bool{}
	)
	if err := tx.QueryRow(ctx, `SELECT code_scan_replace($1, $2, $3, $4, $5)`, projectID, in.Repository, in.Commit, in.Branch, in.FileURL).Scan(&id); err != nil {
		return id, ev, err
	}
	report := func(t FoundTag, p Problem) {
		if len(reported) < MaxReported {
			reported = append(reported, Reported{FoundTag: t, Problem: p})
		} else {
			unreported++
		}
	}
	for _, t := range in.Tags {
		t.Key, t.Path = strings.ToUpper(strings.TrimSpace(t.Key)), strings.TrimSpace(t.Path)
		at := fmt.Sprintf("%s\x00%d\x00%s\x00%s", t.Path, t.Line, t.Word, t.Key)
		if seen[at] {
			continue
		}
		seen[at] = true
		known := relationOf(words, t.Word) != ""
		if !known {
			report(t, ProblemWord)
		}
		tg, found := targets[t.Key]
		was, had := earlier[placed(t.Path, t.Word, t.Key)]
		var req *uuid.UUID
		revision := 0
		switch {
		case !found:
			if known {
				report(t, ProblemUnknown)
				if !had {
					ev.Unknown++
				}
			}
		case tg.section:
			if known {
				report(t, ProblemSection)
			}
			continue
		case t.Revision != nil && (*t.Revision < 1 || *t.Revision > tg.revision):
			report(t, ProblemRevision)
			continue
		default:
			reqID := tg.id
			req = &reqID
			switch {
			case t.Revision != nil:
				revision = *t.Revision
			case had && was.revision >= 1 && was.revision <= tg.revision:
				revision = was.revision
			default:
				revision = tg.revision
			}
			if known && tg.trashed {
				report(t, ProblemTrashed)
				if !had && !slices.Contains(ev.Orphaned, tg.id) {
					ev.Orphaned = append(ev.Orphaned, tg.id)
				}
			}
			if known && !tg.trashed {
				linked++
				if revision < tg.revision && !(had && was.outdated) && !slices.Contains(ev.Outdated, tg.id) {
					ev.Outdated = append(ev.Outdated, tg.id)
				}
			}
		}
		paths, lines, tagWords, keys = append(paths, t.Path), append(lines, t.Line), append(tagWords, t.Word), append(keys, t.Key)
		reqs, revisions = append(reqs, req), append(revisions, revision)
	}
	if len(paths) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO code_tag (scan_id, project_id, path, line, word, key, requirement_id, revision)
			SELECT $1, $2, p, l, w, k, q, v FROM unnest($3::text[], $4::int[], $5::text[], $6::text[], $7::uuid[], $8::int[]) AS x (p, l, w, k, q, v)`,
			id, projectID, paths, lines, tagWords, keys, reqs, revisions); err != nil {
			return id, ev, fmt.Errorf("keep the tags: %w", err)
		}
	}
	body, err := json.Marshal(reported)
	if err != nil {
		return id, ev, err
	}
	_, err = tx.Exec(ctx, `UPDATE code_scan SET tags = $2, linked = $3, reported = $4, unreported = $5 WHERE id = $1`,
		id, len(seen), linked, body, unreported)
	return id, ev, err
}

const selectScan = `
SELECT s.id, p.key, s.repository, s.commit_sha, s.branch, s.file_url, s.tags, s.linked, s.reported, s.unreported, s.uploaded_by,
       COALESCE(u.name, ''), CASE WHEN s.via_kind IS NULL THEN NULL ELSE jsonb_build_object('kind', s.via_kind, 'name', s.via_name) END,
       s.uploaded_at, s.project_id
FROM code_scan s
JOIN project p ON p.id = s.project_id
LEFT JOIN app_user u ON u.id = s.uploaded_by`

func findScan(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Scan, error) {
	out, _, _, err := scanScan(tx.QueryRow(ctx, selectScan+` WHERE s.id = $1`, id))
	return out, err
}

func scanScan(row pgx.Row) (*Scan, *uuid.UUID, uuid.UUID, error) {
	var (
		sc        Scan
		reported  []byte
		by        *uuid.UUID
		v         *via.Via
		projectID uuid.UUID
	)
	err := row.Scan(&sc.ID, &sc.ProjectKey, &sc.Repository, &sc.Commit, &sc.Branch, &sc.FileURL, &sc.Tags, &sc.Linked, &reported,
		&sc.Unreported, &by, &sc.UploadedBy.Name, &v, &sc.UploadedAt, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, projectID, ErrNotFound
	}
	if err != nil {
		return nil, nil, projectID, err
	}
	if by != nil {
		sc.UploadedBy.ID = *by
	}
	sc.Via = v
	if err := json.Unmarshal(reported, &sc.Reported); err != nil {
		return nil, nil, projectID, fmt.Errorf("read what the scan reported: %w", err)
	}
	return &sc, by, projectID, nil
}

// Get is one scan, its uploader's and its project's administrators' to read.
func (s *Service) Get(ctx context.Context, actor, id uuid.UUID) (*Scan, error) {
	var out *Scan
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		sc, by, projectID, err := scanScan(tx.QueryRow(ctx, selectScan+` WHERE s.id = $1`, id))
		if err != nil {
			return err
		}
		if by == nil || *by != actor {
			var administer bool
			if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer')`, projectID, actor).Scan(&administer); err != nil {
				return err
			}
			if !administer {
				return ErrNotFound
			}
		}
		out = sc
		return nil
	})
	return out, err
}

// FileLink is the address of a line at a commit by a scan's template; empty without one.
func FileLink(template, commit, path string, line int) string {
	if template == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.NewReplacer("{commit}", commit, "{path}", strings.Join(parts, "/"), "{line}", strconv.Itoa(line)).Replace(template)
}

// Links are the places that implement or verify a requirement, per repository and branch, the counting branch first.
func (s *Service) Links(ctx context.Context, reader uuid.UUID, reqKey, branch string) ([]Link, string, error) {
	out := []Link{}
	var counting string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, reader, reqKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT evidence_counting_branch($1)`, r.ProjectID).Scan(&counting); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT s.repository, s.branch, s.commit_sha, t.path, t.line, t.word, code_tag_relation(s.project_id, t.word), t.revision,
			       t.revision < q.revision, s.file_url, s.uploaded_at
			FROM code_tag t JOIN code_scan s ON s.id = t.scan_id JOIN requirement q ON q.id = t.requirement_id
			WHERE t.requirement_id = $1 AND code_tag_relation(s.project_id, t.word) IS NOT NULL AND ($2 = '' OR s.branch = $2)
			ORDER BY s.branch <> $3, s.branch, s.repository, t.path, t.line`, r.ID, strings.TrimSpace(branch), counting)
		if err != nil {
			return fmt.Errorf("read the requirement's code: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				l        Link
				template string
			)
			if err := rows.Scan(&l.Repository, &l.Branch, &l.Commit, &l.Path, &l.Line, &l.Word, &l.Relation, &l.Revision, &l.Outdated,
				&template, &l.ScannedAt); err != nil {
				return err
			}
			l.Counts, l.URL = l.Branch == counting, FileLink(template, l.Commit, l.Path, l.Line)
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, counting, err
}

// Tags are a project's tags in one state on a branch, the counting branch when empty, a window at a time.
func (s *Service) Tags(ctx context.Context, reader uuid.UUID, projectKey, branch string, state State, limit, offset int) ([]Tag, int, error) {
	if state != "" && state != Outdated && state != Orphaned {
		return nil, 0, &FieldError{"state", "List the outdated or the orphaned tags, or leave the state out for both."}
	}
	out := []Tag{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if strings.TrimSpace(branch) == "" {
			if err := tx.QueryRow(ctx, `SELECT evidence_counting_branch($1)`, p.ID).Scan(&branch); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `
			WITH tags AS (
				SELECT s.repository, s.branch, s.commit_sha, s.file_url, s.uploaded_at, t.path, t.line, t.word, t.key, t.revision,
				       code_tag_relation(s.project_id, t.word) AS relation, q.id AS req_id, q.number, q.title, q.revision AS current,
				       q.kind, q.trashed_at IS NOT NULL AS trashed,
				       CASE WHEN q.id IS NULL THEN 'unknown' WHEN q.trashed_at IS NOT NULL THEN 'trashed' END AS problem
				FROM code_scan s JOIN code_tag t ON t.scan_id = s.id LEFT JOIN requirement q ON q.id = t.requirement_id
				WHERE s.project_id = $1 AND s.branch = $2
			), kept AS (
				SELECT *, CASE WHEN problem IS NOT NULL THEN 'orphaned' WHEN revision < current THEN 'outdated' ELSE 'current' END AS state
				FROM tags WHERE relation IS NOT NULL
			)
			SELECT repository, branch, commit_sha, file_url, uploaded_at, path, line, word, key, revision, relation, req_id, COALESCE(number, 0),
			       COALESCE(title, ''), COALESCE(current, 0), COALESCE(kind, ''), state, COALESCE(problem, ''), count(*) OVER ()
			FROM kept WHERE state <> 'current' AND ($3 = '' OR state = $3)
			ORDER BY state, repository, path, line LIMIT $4 OFFSET $5`, p.ID, branch, string(state), limit, offset)
		if err != nil {
			return fmt.Errorf("read the project's tags: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				t        Tag
				template string
				reqID    *uuid.UUID
				number   int
				ref      requirement.Ref
			)
			if err := rows.Scan(&t.Repository, &t.Branch, &t.Commit, &template, &t.ScannedAt, &t.Path, &t.Line, &t.Word, &t.Key, &t.Revision,
				&t.Relation, &reqID, &number, &ref.Title, &ref.Revision, &ref.Kind, &t.State, &t.Problem, &total); err != nil {
				return err
			}
			t.Outdated, t.URL = t.State == Outdated, FileLink(template, t.Commit, t.Path, t.Line)
			if reqID != nil {
				ref.ID, ref.ProjectKey, ref.Key = *reqID, p.Key, requirement.Key(p.Key, number)
				ref.Trashed = t.Problem == ProblemTrashed
				t.Requirement = &ref
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, total, err
}
