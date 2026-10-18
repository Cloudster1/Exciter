package testexec

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Report answers what an execution's report holds: the execution, then each case iteration with its steps at the
// pinned revision, what was recorded of them, its files by name and its issues.
func (s *Service) Report(ctx context.Context, reader, id uuid.UUID) (*Report, error) {
	var out *Report
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := readExecution(ctx, tx, reader, id, true)
		if err != nil {
			return err
		}
		rep := &Report{Execution: e.Execution, Cases: []ReportCase{}}
		for _, run := range e.Cases {
			c, err := readCase(ctx, tx, id, run.Case.ID, run.Iteration)
			if err != nil {
				return err
			}
			rc := ReportCase{Run: c.Run, Steps: c.Steps, Results: c.Results, Files: []string{}, Issues: []string{}}
			for _, f := range c.Files {
				rc.Files = append(rc.Files, f.FileName)
			}
			for _, i := range c.Issues {
				rc.Issues = append(rc.Issues, i.Key)
			}
			rep.Cases = append(rep.Cases, rc)
		}
		out = rep
		return nil
	})
	return out, err
}
