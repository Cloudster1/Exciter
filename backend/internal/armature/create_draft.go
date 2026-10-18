package armature

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// IssueDraft is an issue filed with its own words, such as a failure found by a test (#251), and traced with one
// relation to the requirements it names.
type IssueDraft struct {
	ArmatureProject string
	TypeID          *uuid.UUID
	Summary         string
	// Paragraphs are the description's, in order; Link, when set, closes it as a link back to Exciter.
	Paragraphs []string
	Link       string
	LinkText   string
	// RequirementKeys are linked to the new issue with Relation, those the actor may link.
	RequirementKeys []string
	Relation        Relation
}

func summaryOf(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= MaxSummaryLength {
		return s
	}
	return string([]rune(s)[:MaxSummaryLength-len(ellipsis)]) + ellipsis
}

func draftDescription(d IssueDraft) map[string]any {
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	var content []any
	for _, p := range d.Paragraphs {
		if p = strings.TrimSpace(p); p != "" {
			content = append(content, map[string]any{"type": "paragraph", "content": []any{text(p)}})
		}
	}
	if d.Link != "" {
		linked := text(d.LinkText)
		linked["marks"] = []any{map[string]any{"type": "link", "attrs": map[string]any{"href": d.Link}}}
		content = append(content, map[string]any{"type": "paragraph", "content": []any{linked}})
	}
	return map[string]any{"type": "doc", "content": content}
}

// FileDraft files an issue as the actor and links it to each requirement they may link; it answers the issue's key
// and the keys it linked. The issue stands even when a link fails, as an issue made from a requirement does.
func (s *Service) FileDraft(ctx context.Context, d IssueDraft) (string, []string, error) {
	project := strings.ToUpper(strings.TrimSpace(d.ArmatureProject))
	if project == "" {
		return "", nil, &FieldError{Field: "armatureProject", Message: "Choose the Armature project to file the issue in."}
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return "", nil, err
	}
	v, err := s.writer(ctx)
	if err != nil {
		return "", nil, err
	}
	body := CreateRequest{ProjectKey: project, TypeID: d.TypeID, Summary: summaryOf(d.Summary), Description: draftDescription(d)}
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	var answer struct {
		Issue wireIssue `json:"issue"`
	}
	if err := v.Caller.Send(callCtx, http.MethodPost, "/issues", body, &answer); err != nil {
		if errors.Is(err, ErrRejected) {
			s.failed(ctx, v, err)
		}
		return "", nil, err
	}
	issue := answer.Issue.issue(v.Caller.BaseURL())
	s.cache.PutIssue(ctx, v.TokenID, issue.Key, issue)
	var linked []string
	for _, key := range d.RequirementKeys {
		if _, _, err := s.traceNew(ctx, actor, key, d.Relation, issue); err == nil {
			linked = append(linked, key)
		}
	}
	return issue.Key, linked, nil
}
