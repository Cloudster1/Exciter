package armature

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Issues made from requirements (#28), Stator's create with each issue traced to its requirement at once.

// issueWrite is the Armature permission to file issues in a project.
const issueWrite = "issue.write"

// CreateRelations are the relations a new issue may take: it was made to do or to check the requirement.
var CreateRelations = []Relation{Implements, Verifies}

// IssueSummary is how an issue made from a requirement is summarized: its key and its title, the title cut to
// Armature's bound and ending in three dots.
func IssueSummary(reqKey, title string) string {
	full := reqKey + " " + strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(full) <= MaxSummaryLength {
		return full
	}
	return string([]rune(full)[:MaxSummaryLength-len(ellipsis)]) + ellipsis
}

const ellipsis = "..."

// Description is the one paragraph an issue made from a requirement holds: "From {KEY} in Exciter", the key
// linked to the requirement.
func Description(reqKey, url string) map[string]any {
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	linked := text(reqKey)
	linked["marks"] = []any{map[string]any{"type": "link", "attrs": map[string]any{"href": url}}}
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{text("From "), linked, text(" in Exciter")}},
	}}
}

// CreateRequest is the body Exciter sends to Armature's POST /issues.
type CreateRequest struct {
	ProjectKey  string         `json:"projectKey"`
	TypeID      *uuid.UUID     `json:"typeId,omitempty"`
	Summary     string         `json:"summary"`
	Description map[string]any `json:"description,omitempty"`
}

// CheckCreate refuses what Armature need not be asked about, and tidies the keys in place: upper case, each once.
func CheckCreate(in *CreateInput) error {
	in.ArmatureProject = strings.ToUpper(strings.TrimSpace(in.ArmatureProject))
	if in.ArmatureProject == "" {
		return &FieldError{Field: "armatureProject", Message: "Choose the Armature project to file the issues in."}
	}
	if !slices.Contains(CreateRelations, in.Relation) {
		return &FieldError{Field: "relation", Message: "Say whether the new issues implement or verify their requirements."}
	}
	keys := make([]string, 0, len(in.RequirementKeys))
	for _, raw := range in.RequirementKeys {
		key := strings.ToUpper(strings.TrimSpace(raw))
		if key == "" {
			return &FieldError{Field: "requirementKeys", Message: "Name every requirement by its key, such as CHK-R1."}
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 || len(keys) > MaxCreateItems {
		return &FieldError{Field: "requirementKeys", Message: "Choose 1 to " + strconv.Itoa(MaxCreateItems) + " requirements, and make issues for the rest in another round."}
	}
	in.RequirementKeys = keys
	return nil
}

// writer is the viewer when they may file issues and links in Armature, else why not.
func (s *Service) writer(ctx context.Context) (*Viewer, error) {
	v, status, err := s.Viewer(ctx)
	switch {
	case err != nil:
		return nil, err
	case status == StatusNotConfigured:
		return nil, ErrNotConfigured
	case status == StatusNotConnected:
		return nil, ErrNotConnected
	case status == StatusUnreachable:
		return nil, ErrUnreachable
	case v == nil:
		return nil, ErrRejected
	case v.ReadOnly:
		return nil, ErrReadOnlyToken
	}
	return v, nil
}

// creatable reads the requirements keys name, in order, refusing the whole round when one is not in the project or
// not the actor's to link.
func creatable(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string, keys []string) ([]*requirement.Endpoint, error) {
	var projectID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM project WHERE key = $1
		UNION ALL SELECT project_id FROM project_key_alias WHERE key = $1
		LIMIT 1`, strings.ToUpper(projectKey)).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, project.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find the project: %w", err)
	}
	out := make([]*requirement.Endpoint, 0, len(keys))
	for _, key := range keys {
		r, err := linkable(ctx, tx, actor, key)
		if errors.Is(err, requirement.ErrNotFound) || (err == nil && r.ProjectID != projectID) {
			return nil, &FieldError{Field: "requirementKeys", Message: "This project has no requirement " + key + ". Choose the requirements again."}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// CreateIssues files one issue per requirement, in order, as the actor, and traces each requirement to its new
// issue at once. It stops at the first refusal: what was made, and an ItemError naming the requirement.
func (s *Service) CreateIssues(ctx context.Context, projectKey string, in CreateInput) ([]Created, db.LSN, error) {
	if err := CheckCreate(&in); err != nil {
		return nil, 0, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var reqs []*requirement.Endpoint
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		reqs, err = creatable(ctx, tx, actor, projectKey, in.RequirementKeys)
		return err
	}); err != nil {
		return nil, 0, err
	}
	v, err := s.writer(ctx)
	if err != nil {
		return nil, 0, err
	}
	made := []Created{}
	var last db.LSN
	for _, r := range reqs {
		issue, err := s.file(ctx, v, in, r)
		if err != nil {
			return made, last, &ItemError{RequirementKey: r.Key, Err: err}
		}
		link, lsn, err := s.traceNew(ctx, actor, r.Key, in.Relation, issue)
		last = max(last, lsn)
		if err != nil {
			return made, last, &ItemError{RequirementKey: r.Key, IssueKey: issue.Key, Err: err}
		}
		made = append(made, Created{RequirementKey: r.Key, Issue: *issue, Link: *link})
	}
	return made, last, nil
}

// ItemError is the requirement a round stopped at. IssueKey is set when Armature made the issue and Exciter then
// could not link it, as when the requirement went to the trash meanwhile: the issue stands.
type ItemError struct {
	RequirementKey string
	IssueKey       string
	Err            error
}

func (e *ItemError) Error() string {
	if e.IssueKey != "" {
		return fmt.Sprintf("Armature made %s, and linking it to %s failed: %v", e.IssueKey, e.RequirementKey, e.Err)
	}
	return fmt.Sprintf("no issue was made for %s: %v", e.RequirementKey, e.Err)
}

func (e *ItemError) Unwrap() error { return e.Err }

// file is Armature's POST /issues for one requirement, as the viewer.
func (s *Service) file(ctx context.Context, v *Viewer, in CreateInput, r *requirement.Endpoint) (*Issue, error) {
	body := CreateRequest{
		ProjectKey:  in.ArmatureProject,
		TypeID:      in.TypeID,
		Summary:     IssueSummary(r.Key, r.Title),
		Description: Description(r.Key, strings.TrimSuffix(s.opts.AppURL, "/")+RequirementPath+r.ID.String()),
	}
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	var answer struct {
		Issue wireIssue `json:"issue"`
	}
	if err := v.Caller.Send(callCtx, http.MethodPost, "/issues", body, &answer); err != nil {
		if errors.Is(err, ErrRejected) {
			// Marked, so the token is not sent again until its owner stores a new one.
			s.failed(ctx, v, err)
		}
		return nil, err
	}
	issue := answer.Issue.issue(v.Caller.BaseURL())
	// The new chip shows at once; a null kept from before the issue existed would hide it until it expired.
	s.cache.PutIssue(ctx, v.TokenID, issue.Key, issue)
	return issue, nil
}

// traceNew links a requirement to the issue just made for it, asked again whether the actor may, as Link does.
func (s *Service) traceNew(ctx context.Context, actor uuid.UUID, reqKey string, relation Relation, issue *Issue) (*Link, db.LSN, error) {
	var out *Link
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := linkable(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO armature_issue_link (requirement_id, relation, issue_key, created_by)
			VALUES ($1, $2, $3, $4) RETURNING id`, r.ID, relation, issue.Key, actor).Scan(&id); err != nil {
			return fmt.Errorf("link the new Armature issue: %w", err)
		}
		links, err := readLinks(ctx, tx, r.ID, &id)
		if err != nil || len(links) == 0 {
			return errors.Join(err, ErrLinkNotFound)
		}
		out = &links[0]
		return nil
	})
	if err != nil {
		return nil, lsn, err
	}
	s.noteStatus(ctx, map[string]*Issue{issue.Key: issue})
	out.StatusCategory = issue.Status.Category
	return out, lsn, nil
}

// meta is what MetaKey holds for a person.
type meta struct {
	Projects   []Project   `json:"projects"`
	IssueTypes []IssueType `json:"issueTypes"`
}

// Projects are the Armature projects the viewer may see, archived ones left out, each saying whether they may file
// issues in it.
func (s *Service) Projects(ctx context.Context) (Status, []Project, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	m, err := s.meta(ctx, v)
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	return StatusOK, m.Projects, nil
}

// IssueTypes are Armature's issue types a new issue may take, sub-tasks left out, from the same entry as Projects.
func (s *Service) IssueTypes(ctx context.Context) (Status, []IssueType, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	m, err := s.meta(ctx, v)
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	return StatusOK, m.IssueTypes, nil
}

// meta is the viewer's projects and issue types, cached together for MetaCacheTTL, since the create dialog reads
// both and no webhook announces a change to either.
func (s *Service) meta(ctx context.Context, v *Viewer) (*meta, error) {
	var m meta
	if s.cache.Meta(ctx, v.TokenID, &m) {
		return &m, nil
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	var projects struct {
		Projects []struct {
			Key        string     `json:"key"`
			Name       string     `json:"name"`
			ArchivedAt *time.Time `json:"archivedAt"`
		} `json:"projects"`
	}
	if err := v.Caller.Get(ctx, "/projects", nil, &projects); err != nil {
		return nil, err
	}
	var access struct {
		Permissions struct {
			Org      []string            `json:"org"`
			Projects map[string][]string `json:"projects"`
		} `json:"permissions"`
	}
	if err := v.Caller.Get(ctx, "/access/me", nil, &access); err != nil {
		return nil, err
	}
	var types struct {
		IssueTypes []struct {
			IssueType
			IsSubtask bool `json:"isSubtask"`
		} `json:"issueTypes"`
	}
	if err := v.Caller.Get(ctx, "/issue-types", nil, &types); err != nil {
		return nil, err
	}
	m.Projects = []Project{}
	for _, p := range projects.Projects {
		if p.ArchivedAt != nil {
			continue
		}
		m.Projects = append(m.Projects, Project{
			Key:  p.Key,
			Name: p.Name,
			// A read token files nothing, whatever its owner may do.
			CanCreate: !v.ReadOnly && (slices.Contains(access.Permissions.Org, issueWrite) || slices.Contains(access.Permissions.Projects[p.Key], issueWrite)),
		})
	}
	m.IssueTypes = []IssueType{}
	for _, t := range types.IssueTypes {
		if !t.IsSubtask {
			m.IssueTypes = append(m.IssueTypes, t.IssueType)
		}
	}
	s.cache.PutMeta(ctx, v.TokenID, m)
	return &m, nil
}
