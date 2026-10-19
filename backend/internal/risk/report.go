package risk

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// MaxReportRisks is the most risks one report lists, as an export holds at most so many rows.
const MaxReportRisks = 2000

// Report reads the risk report (#261): the scheme, how many risks stand at each level before mitigation and in
// effect, and every risk the filter keeps with its ratings and mitigations.
func (s *Service) Report(ctx context.Context, reader uuid.UUID, projectKey string, f search.Filter, sort search.Sort) (*Report, error) {
	out := &Report{Levels: []LevelCount{}, Risks: []Row{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out.ProjectKey, out.ProjectName = p.Key, p.Name
		scheme, err := ReadScheme(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if scheme == nil {
			return ErrNoScheme
		}
		out.Scheme = *scheme
		all, err := readRisks(ctx, tx, reader, p, f, sort)
		if err != nil {
			return err
		}
		counts := map[string]*LevelCount{}
		for _, l := range scheme.Levels {
			out.Levels = append(out.Levels, LevelCount{Level: requirement.RiskLevel{Name: l.Name, Colour: l.Colour, Acceptable: l.Acceptable}})
		}
		for i := range out.Levels {
			counts[out.Levels[i].Level.Name] = &out.Levels[i]
		}
		for _, r := range all {
			if r.view.BeforeLevel == nil {
				out.Unrated++
			} else if c := counts[r.view.BeforeLevel.Name]; c != nil {
				c.Before++
			}
			if r.view.Level != nil {
				if c := counts[r.view.Level.Name]; c != nil {
					c.Effective++
				}
			}
			if len(out.Risks) < MaxReportRisks {
				out.Risks = append(out.Risks, r.row)
			}
		}
		return nil
	})
	return out, err
}
