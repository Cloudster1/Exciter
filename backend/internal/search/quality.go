package search

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/quality"
)

// Quality sums up the writing rules over the requirements of a project that f keeps (#207): how many score in each
// band, how many requirements break each rule, and the requirements lowest first, a window at a time.
func (s *Service) Quality(ctx context.Context, reader uuid.UUID, projectKey string, f Filter, limit, offset int) (*quality.Summary, error) {
	var out *quality.Summary
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		kept, err := Keep(ctx, tx, reader, p, f, Sort{})
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(kept))
		for i, k := range kept {
			ids[i] = k.Ref.ID
		}
		rows, err := tx.Query(ctx, `
			SELECT x.id, q.score, q.active, q.accepted, q.rules, q.stale
			FROM unnest($1::uuid[]) AS x(id) CROSS JOIN LATERAL requirement_quality_of(x.id) q`, ids)
		if err != nil {
			return fmt.Errorf("read the scores: %w", err)
		}
		scored := map[uuid.UUID]quality.Scored{}
		for rows.Next() {
			var (
				id    uuid.UUID
				sc    quality.Scored
				rules []string
			)
			if err := rows.Scan(&id, &sc.Score, &sc.Active, &sc.Accepted, &rules, &sc.Stale); err != nil {
				rows.Close()
				return err
			}
			sc.Rules = make([]quality.RuleID, len(rules))
			for i, r := range rules {
				sc.Rules[i] = quality.RuleID(r)
			}
			scored[id] = sc
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		out = summarize(kept, scored, limit, offset)
		return nil
	})
	return out, err
}

func summarize(kept []Kept, scored map[uuid.UUID]quality.Scored, limit, offset int) *quality.Summary {
	out := &quality.Summary{Bands: []quality.Band{}, Rules: []quality.RuleCount{}, Requirements: []quality.Scored{}, Limit: limit, Offset: offset}
	for from := 0; from < quality.FullScore; from += quality.BandWidth {
		to := from + quality.BandWidth - 1
		if to+quality.BandWidth >= quality.FullScore {
			to = quality.FullScore
		}
		out.Bands = append(out.Bands, quality.Band{From: from, To: to})
		if to == quality.FullScore {
			break
		}
	}
	counts := map[quality.RuleID]int{}
	all := make([]quality.Scored, 0, len(kept))
	sum := 0
	for _, k := range kept {
		sc := scored[k.Ref.ID]
		sc.ID, sc.Key, sc.Title = k.Ref.ID, k.Ref.Key, k.Ref.Title
		if sc.Rules == nil {
			sc.Rules = []quality.RuleID{}
		}
		all = append(all, sc)
		if sc.Score == nil {
			out.Unchecked++
			continue
		}
		out.Checked++
		sum += *sc.Score
		for i := range out.Bands {
			if *sc.Score >= out.Bands[i].From && *sc.Score <= out.Bands[i].To {
				out.Bands[i].Count++
			}
		}
		for _, r := range sc.Rules {
			counts[r]++
		}
	}
	if out.Checked > 0 {
		avg := float64(sum) / float64(out.Checked)
		out.Average = &avg
	}
	for _, id := range quality.RuleIDs {
		if counts[id] > 0 {
			out.Rules = append(out.Rules, quality.RuleCount{Rule: id, Count: counts[id]})
		}
	}
	slices.SortStableFunc(out.Rules, func(a, b quality.RuleCount) int { return cmp.Compare(b.Count, a.Count) })
	// Lowest first; a requirement not checked yet comes after every score.
	slices.SortStableFunc(all, func(a, b quality.Scored) int {
		switch {
		case a.Score == nil && b.Score == nil:
			return 0
		case a.Score == nil:
			return 1
		case b.Score == nil:
			return -1
		}
		return cmp.Compare(*a.Score, *b.Score)
	})
	out.Total = len(all)
	if offset < len(all) {
		out.Requirements = all[offset:min(len(all), offset+limit)]
	}
	return out
}
