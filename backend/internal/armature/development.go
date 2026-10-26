package armature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/testresult"
)

// The development panel of an issue (#200): Armature's GET /issues/{key}/development names the commits that name
// the issue, which a test result's commit is matched against so it links to where Armature shows it.

type wireCommit struct {
	SHA string `json:"sha"`
	URL string `json:"url"`
}

// Commits are the commits the development panels of a requirement's linked issues name, by SHA, read as the
// reader; empty when the reader has no Armature token, and an issue Armature does not show answers none.
func (s *Service) Commits(ctx context.Context, reader uuid.UUID, requirementID uuid.UUID) (map[string]testresult.CommitLink, error) {
	out := map[string]testresult.CommitLink{}
	v, status, err := s.Viewer(db.WithUser(ctx, reader))
	if err != nil || status != StatusOK {
		return out, err
	}
	var keys []string
	if err := s.db.Read(db.WithUser(ctx, reader), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT issue_key FROM armature_issue_link
			WHERE requirement_id = $1 AND broken_at IS NULL AND detached_at IS NULL ORDER BY issue_key`, requirementID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	}); err != nil || len(keys) == 0 {
		return out, err
	}
	for _, key := range keys {
		var answer struct {
			Commits []wireCommit `json:"commits"`
		}
		err := v.Caller.Get(ctx, "/issues/"+url.PathEscape(key)+"/development", nil, &answer)
		var refused *RefusedError
		if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden) {
			continue
		}
		if err != nil {
			return out, err
		}
		addCommits(out, key, answer.Commits)
	}
	return out, nil
}

// addCommits keeps the first issue naming each commit, and only commits whose address is a web address, since
// it is shown as a link.
func addCommits(out map[string]testresult.CommitLink, key string, commits []wireCommit) {
	for _, c := range commits {
		sha := strings.ToLower(strings.TrimSpace(c.SHA))
		if sha == "" || !webAddress(c.URL) {
			continue
		}
		if _, seen := out[sha]; !seen {
			out[sha] = testresult.CommitLink{IssueKey: key, URL: c.URL}
		}
	}
}

func webAddress(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
