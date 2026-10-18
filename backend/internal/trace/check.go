package trace

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// MaxCheckItems bounds each list a trace check answers; the rest are counted (#224).
const MaxCheckItems = 500

// Verdict is a trace check's answer, for a pipeline to stop on.
type Verdict string

const (
	Pass Verdict = "pass"
	Fail Verdict = "fail"
)

// Verdicts lists every Verdict, for the API document.
var Verdicts = []Verdict{Pass, Fail}

// CheckReason is what a trace check fails on when asked.
type CheckReason string

const (
	FailBroken   CheckReason = "broken"
	FailGaps     CheckReason = "gaps"
	FailFailing  CheckReason = "failing"
	FailOutdated CheckReason = "outdated"
	FailOrphaned CheckReason = "orphaned"
)

// CheckReasons lists every CheckReason; DefaultCheckFail is what a check fails on unless asked otherwise.
var (
	CheckReasons     = []CheckReason{FailBroken, FailGaps, FailFailing, FailOutdated, FailOrphaned}
	DefaultCheckFail = []CheckReason{FailBroken, FailGaps, FailFailing}
)

// EvidenceKind says whether evidence is a test result or a code tag.
type EvidenceKind string

const (
	EvidenceTest EvidenceKind = "test"
	EvidenceCode EvidenceKind = "code"
)

// EvidenceKinds lists every EvidenceKind, for the API document.
var EvidenceKinds = []EvidenceKind{EvidenceTest, EvidenceCode}

// CheckBroken is a chain that stops short: its root, the keys it reached and the level it did not.
type CheckBroken struct {
	Root    requirement.Ref `json:"root"`
	Path    []string        `json:"path"`
	Missing string          `json:"missing"`
}

// CheckGap is a requirement whose coverage asks for a dimension it does not hold in full.
type CheckGap struct {
	Requirement requirement.Ref `json:"requirement"`
	Dimension   Dimension       `json:"dimension"`
	Level       Level           `json:"level"`
}

// CheckEvidence is a failing or outdated test result, or an outdated code tag, of a requirement.
type CheckEvidence struct {
	Requirement requirement.Ref `json:"requirement"`
	Kind        EvidenceKind    `json:"kind"`
	// Name is the test's name, or the path and line of the tag.
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Outcome    string `json:"outcome,omitempty"`
	Revision   int    `json:"revision"`
	Commit     string `json:"commit"`
	URL        string `json:"url"`
}

// CheckOrphan is a code tag naming a requirement in the trash or none at all.
type CheckOrphan struct {
	Key        string `json:"key"`
	Problem    string `json:"problem"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	URL        string `json:"url"`
}

// CheckTotals counts each list in whole, before MaxCheckItems cuts it.
type CheckTotals struct {
	Broken   int `json:"broken"`
	Gaps     int `json:"gaps"`
	Failing  int `json:"failing"`
	Outdated int `json:"outdated"`
	Orphaned int `json:"orphaned"`
}

// CheckEvidenceSeen says how much evidence the branch had, so a pipeline sees a check over nothing.
type CheckEvidenceSeen struct {
	Runs  int `json:"runs"`
	Scans int `json:"scans"`
}

// Check is a trace check's verdict and what it found, over the requirements a query keeps on one branch.
type Check struct {
	Verdict Verdict       `json:"verdict"`
	Branch  string        `json:"branch"`
	Fail    []CheckReason `json:"fail"`
	// Chains is false when the project has no trace model, so no chain was followed.
	Chains   bool              `json:"chains"`
	Broken   []CheckBroken     `json:"broken"`
	Gaps     []CheckGap        `json:"gaps"`
	Failing  []CheckEvidence   `json:"failing"`
	Outdated []CheckEvidence   `json:"outdated"`
	Orphaned []CheckOrphan     `json:"orphaned"`
	Totals   CheckTotals       `json:"totals"`
	Evidence CheckEvidenceSeen `json:"evidence"`
	Checked  int               `json:"checked"`
}

// CheckQuery is what a trace check judges: the requirements the grid's filter keeps, on a branch, failing on the
// reasons asked; the counting branch and DefaultCheckFail when they are empty.
type CheckQuery struct {
	Filter search.Filter
	Branch string
	Fail   []CheckReason
}

// MaxCheckChainLines bounds the chain lines a check follows, as an export bounds its rows.
const MaxCheckChainLines = 20000

// Check judges the trace of the requirements a filter keeps for a pipeline: broken chains, coverage gaps and failing
// or outdated test results, each a reason to fail when asked.
func (s *Service) Check(ctx context.Context, reader uuid.UUID, projectKey string, q CheckQuery) (*Check, error) {
	fail := q.Fail
	if len(fail) == 0 {
		fail = DefaultCheckFail
	}
	out := &Check{Fail: fail, Broken: []CheckBroken{}, Gaps: []CheckGap{}, Failing: []CheckEvidence{}, Outdated: []CheckEvidence{},
		Orphaned: []CheckOrphan{}}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if err := evidenceOn(ctx, tx, q.Branch); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT evidence_branch($1)`, p.ID).Scan(&out.Branch); err != nil {
			return err
		}
		kept, err := search.Keep(ctx, tx, reader, p, q.Filter, search.Sort{})
		if err != nil {
			return err
		}
		out.Checked = len(kept)
		rows, err := judge(ctx, tx, p.ID, kept)
		if err != nil {
			return err
		}
		for _, r := range rows {
			for _, d := range []struct {
				dim   Dimension
				level Level
			}{{Decomposed, r.Decomposed}, {Implemented, r.Implemented}, {Verified, r.Verified}} {
				if d.level == LevelNone || d.level == LevelPartial {
					out.Totals.Gaps++
					if len(out.Gaps) < MaxCheckItems {
						out.Gaps = append(out.Gaps, CheckGap{Requirement: r.Requirement, Dimension: d.dim, Level: d.level})
					}
				}
			}
		}
		if err := checkTests(ctx, tx, kept, out); err != nil {
			return err
		}
		if err := checkCode(ctx, tx, p, kept, out); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM test_run WHERE project_id = $1 AND branch = $2 AND state = 'done'),
			       (SELECT count(*) FROM code_scan WHERE project_id = $1 AND branch = $2 AND replaced_at IS NULL)`,
			p.ID, out.Branch).Scan(&out.Evidence.Runs, &out.Evidence.Scans)
	})
	if err != nil {
		return nil, err
	}
	chains, err := s.Chains(ctx, reader, projectKey, ChainQuery{Filter: q.Filter, Branch: q.Branch, OnlyBroken: true,
		Limit: MaxCheckChainLines, MaxLines: MaxCheckChainLines, Truncate: true})
	if err != nil {
		return nil, err
	}
	out.Chains = len(chains.Levels) > 0
	for _, line := range chains.Lines {
		out.Totals.Broken++
		if len(out.Broken) == MaxCheckItems {
			continue
		}
		b := CheckBroken{Root: chains.Roots[line.Root], Path: make([]string, 0, len(line.Nodes))}
		for _, n := range line.Nodes {
			b.Path = append(b.Path, nodeKey(n))
		}
		if len(line.Nodes) < len(chains.Levels) {
			b.Missing = chains.Levels[len(line.Nodes)].Name
		} else if last := line.Nodes[len(line.Nodes)-1]; last.Code != nil {
			b.Missing = "current code"
		} else {
			b.Missing = "passing test"
		}
		out.Broken = append(out.Broken, b)
	}
	out.Verdict = Pass
	for _, reason := range fail {
		if out.Totals.of(reason) > 0 {
			out.Verdict = Fail
		}
	}
	return out, nil
}

// of is the count of what fails for one reason.
func (t CheckTotals) of(r CheckReason) int {
	switch r {
	case FailBroken:
		return t.Broken
	case FailGaps:
		return t.Gaps
	case FailFailing:
		return t.Failing
	case FailOutdated:
		return t.Outdated
	case FailOrphaned:
		return t.Orphaned
	}
	return 0
}

// evidenceOn has the transaction read evidence on a branch other than the counting one.
func evidenceOn(ctx context.Context, tx db.DBTX, branch string) error {
	if branch == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT set_config('app.evidence_branch', $1, true)`, branch)
	return err
}

// checkTests finds the latest results of the requirements kept that failed or were made against an older revision.
func checkTests(ctx context.Context, tx db.DBTX, kept []search.Kept, out *Check) error {
	if len(kept) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(kept))
	refs := map[uuid.UUID]requirement.Ref{}
	for i, k := range kept {
		ids[i], refs[k.Ref.ID] = k.Ref.ID, k.Ref
	}
	rows, err := tx.Query(ctx, `
		SELECT tr.requirement_id, tr.suite, tr.name, l.repository, tr.outcome, tr.revision, tr.revision < r.revision, t.commit_sha, t.run_url
		FROM unnest($1::uuid[]) WITH ORDINALITY AS k (id, place)
		JOIN requirement r ON r.id = k.id
		JOIN test_run_latest l ON l.project_id = r.project_id AND l.branch = evidence_branch(r.project_id)
		JOIN test_result tr ON tr.run_id = l.run_id AND tr.requirement_id = r.id
		JOIN test_run t ON t.id = tr.run_id
		WHERE tr.outcome IN ('failed', 'errored') OR tr.revision < r.revision
		ORDER BY k.place, l.repository, tr.suite, tr.name`, ids)
	if err != nil {
		return fmt.Errorf("read the failing and outdated tests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id          uuid.UUID
			suite, name string
			e           CheckEvidence
			outdated    bool
		)
		if err := rows.Scan(&id, &suite, &name, &e.Repository, &e.Outcome, &e.Revision, &outdated, &e.Commit, &e.URL); err != nil {
			return err
		}
		e.Requirement, e.Kind, e.Name = refs[id], EvidenceTest, name
		if suite != "" && suite != name {
			e.Name = suite + " " + name
		}
		if e.Outcome == "failed" || e.Outcome == "errored" {
			out.Totals.Failing++
			if len(out.Failing) < MaxCheckItems {
				out.Failing = append(out.Failing, e)
			}
		}
		if outdated {
			out.Totals.Outdated++
			if len(out.Outdated) < MaxCheckItems {
				out.Outdated = append(out.Outdated, e)
			}
		}
	}
	return rows.Err()
}

// checkCode finds the outdated tags of the requirements kept, and the orphaned tags of the branch: those naming a
// requirement in the trash, kept or not, and those naming none, which no filter can keep.
func checkCode(ctx context.Context, tx db.DBTX, p *project.Project, kept []search.Kept, out *Check) error {
	ids := make([]uuid.UUID, len(kept))
	refs := map[uuid.UUID]requirement.Ref{}
	for i, k := range kept {
		ids[i], refs[k.Ref.ID] = k.Ref.ID, k.Ref
	}
	rows, err := tx.Query(ctx, `
		SELECT t.requirement_id, s.repository, t.path, t.line, t.key, t.revision, s.file_url, s.commit_sha,
		       CASE WHEN q.id IS NULL THEN 'unknown' WHEN q.trashed_at IS NOT NULL THEN 'trashed' END,
		       q.id IS NOT NULL AND q.trashed_at IS NULL AND t.revision < q.revision AND q.id = ANY ($3)
		FROM code_scan s JOIN code_tag t ON t.scan_id = s.id LEFT JOIN requirement q ON q.id = t.requirement_id
		WHERE s.project_id = $1 AND s.branch = $2 AND code_tag_relation(s.project_id, t.word) IS NOT NULL
		  AND (q.id IS NULL OR q.trashed_at IS NOT NULL OR (t.revision < q.revision AND q.id = ANY ($3)))
		ORDER BY s.repository, t.path, t.line`, p.ID, out.Branch, ids)
	if err != nil {
		return fmt.Errorf("read the outdated and orphaned tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id                              *uuid.UUID
			repository, path, key, template string
			commit                          string
			line, revision                  int
			problem                         *string
			outdated                        bool
		)
		if err := rows.Scan(&id, &repository, &path, &line, &key, &revision, &template, &commit, &problem, &outdated); err != nil {
			return err
		}
		link := codetrace.FileLink(template, commit, path, line)
		switch {
		case problem != nil:
			out.Totals.Orphaned++
			if len(out.Orphaned) < MaxCheckItems {
				out.Orphaned = append(out.Orphaned, CheckOrphan{Key: key, Problem: *problem, Repository: repository, Path: path, Line: line, URL: link})
			}
		case outdated && id != nil:
			out.Totals.Outdated++
			if len(out.Outdated) < MaxCheckItems {
				out.Outdated = append(out.Outdated, CheckEvidence{Requirement: refs[*id], Kind: EvidenceCode, Name: path + ":" + strconv.Itoa(line),
					Repository: repository, Revision: revision, Commit: commit, URL: link})
			}
		}
	}
	return rows.Err()
}

// nodeKey names a chain's node as a pipeline prints it: a requirement's or an issue's key, a page's address.
func nodeKey(n ChainNode) string {
	switch {
	case n.Requirement != nil:
		return n.Requirement.Key
	case n.Page != nil:
		return n.Page.URL
	case n.Test != nil:
		return n.Test.Name + " " + n.Test.Outcome
	case n.Code != nil:
		return n.Code.Path + ":" + strconv.Itoa(n.Code.Line)
	}
	return n.IssueKey
}
