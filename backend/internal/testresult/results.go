package testresult

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// CommitFinder says where Armature shows the commits a requirement's linked issues name, read as the reader; nil
// when Armature is not connected.
type CommitFinder interface {
	Commits(ctx context.Context, reader uuid.UUID, requirementID uuid.UUID) (map[string]CommitLink, error)
}

// WithCommits has results name where Armature shows their commit.
func (s *Service) WithCommits(f CommitFinder) *Service {
	s.commits = f
	return s
}

// Results are the latest result of each test that verifies a requirement, per repository, branch and job, the
// counting branch first; branch keeps one.
func (s *Service) Results(ctx context.Context, reader uuid.UUID, reqKey, branch string) ([]Result, string, error) {
	var (
		out      []Result
		counting string
		reqID    uuid.UUID
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, reader, reqKey)
		if err != nil {
			return err
		}
		reqID = r.ID
		set, err := settingsOf(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		counting = set.CountingBranch
		rows, err := tx.Query(ctx, `
			SELECT tr.name, tr.suite, l.repository, l.branch, l.job, tr.outcome, tr.message, tr.duration_ms, tr.revision,
			       tr.revision < q.revision, t.commit_sha, t.id, t.run_url, t.uploaded_at
			FROM test_result tr
			JOIN test_run_latest l ON l.run_id = tr.run_id
			JOIN test_run t ON t.id = tr.run_id
			JOIN requirement q ON q.id = tr.requirement_id
			WHERE tr.requirement_id = $1 AND ($2 = '' OR l.branch = $2)
			ORDER BY l.branch <> $3, l.branch, l.repository, l.job, tr.suite, tr.name`, r.ID, strings.TrimSpace(branch), counting)
		if err != nil {
			return fmt.Errorf("read the requirement's results: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var x Result
			if err := rows.Scan(&x.Test, &x.Suite, &x.Repository, &x.Branch, &x.Job, &x.Outcome, &x.Message, &x.DurationMs, &x.Revision,
				&x.Outdated, &x.Commit, &x.RunID, &x.RunURL, &x.At); err != nil {
				return err
			}
			x.Counts = x.Branch == counting
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", err
	}
	if out == nil {
		out = []Result{}
	}
	if s.commits != nil && len(out) > 0 {
		// Armature unreachable leaves the results as they are, without its links.
		if known, err := s.commits.Commits(ctx, reader, reqID); err == nil {
			for i := range out {
				out[i].Armature = matchCommit(known, out[i].Commit)
			}
		}
	}
	return out, counting, nil
}

// matchCommit finds a commit by its SHA, either side possibly short.
func matchCommit(known map[string]CommitLink, sha string) *CommitLink {
	for k, link := range known {
		if strings.HasPrefix(k, sha) || strings.HasPrefix(sha, k) {
			return &link
		}
	}
	return nil
}

// RetentionInterval is how often the worker prunes old runs.
const RetentionInterval = time.Hour

// Retention prunes old runs as the admin role: beyond KeptRuns per repository, branch and job, never the latest, and
// every run of a branch that does not count once it was idle for IdleBranchDays.
type Retention struct {
	db  *db.Cluster
	log *slog.Logger
}

func NewRetention(cluster *db.Cluster, log *slog.Logger) *Retention {
	return &Retention{db: cluster, log: log}
}

// Run prunes on start and then every RetentionInterval until ctx ends.
func (r *Retention) Run(ctx context.Context) {
	ticker := time.NewTicker(RetentionInterval)
	defer ticker.Stop()
	for {
		n, err := r.Once(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Warn("old test runs could not all be pruned", "error", err)
		}
		if n > 0 {
			r.log.Info("old test runs pruned", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once prunes what is past keeping and says how many runs went.
func (r *Retention) Once(ctx context.Context) (int64, error) {
	var n int64
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			DELETE FROM test_run t
			USING (SELECT id, row_number() OVER (PARTITION BY project_id, repository, branch, job ORDER BY uploaded_at DESC, id DESC) AS place
			       FROM test_run WHERE state IN ('done', 'failed')) o
			WHERE o.id = t.id AND o.place > $1 AND NOT EXISTS (SELECT 1 FROM test_run_latest l WHERE l.run_id = t.id)`, KeptRuns)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		tag, err = tx.Exec(ctx, `
			DELETE FROM test_run t
			WHERE t.branch <> evidence_counting_branch(t.project_id)
			  AND NOT EXISTS (SELECT 1 FROM test_run n WHERE n.project_id = t.project_id AND n.repository = t.repository
			                  AND n.branch = t.branch AND n.uploaded_at > now() - make_interval(days => $1))`, IdleBranchDays)
		n += tag.RowsAffected()
		return err
	})
	return n, err
}
