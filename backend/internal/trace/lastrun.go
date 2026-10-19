package trace

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// RunOutcome is a test case's last result, as test_case_last_run answers it; none when it has none.
type RunOutcome string

// RunOutcomes lists every RunOutcome, worst first.
var RunOutcomes = []RunOutcome{"failed", "blocked", "none", "skipped", "passed"}

// LastRun is a test case's last result (#252): its outcome, the execution that recorded it, and whether that ran an
// older revision than the case's current one.
type LastRun struct {
	Outcome     RunOutcome `json:"outcome"`
	ExecutionID *uuid.UUID `json:"executionId"`
	Outdated    bool       `json:"outdated"`
}

// lastRuns reads the last result of each of the requirements given that is a test case, as the reader sees it.
func lastRuns(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (map[uuid.UUID]*LastRun, error) {
	out := map[uuid.UUID]*LastRun{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT r.id, COALESCE(x.outcome, 'none'), x.execution_id, COALESCE(x.revision < r.revision, false)
		FROM requirement r
		JOIN requirement_type ty ON ty.id = r.type_id AND ty.role = 'testCase'
		LEFT JOIN LATERAL test_case_last_run(r.id) x ON true
		WHERE r.id = ANY ($1) AND r.kind = 'requirement'`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the test cases' last results: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id  uuid.UUID
			run LastRun
		)
		if err := rows.Scan(&id, &run.Outcome, &run.ExecutionID, &run.Outdated); err != nil {
			return nil, err
		}
		out[id] = &run
	}
	return out, rows.Err()
}
