package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/keyset"
)

const (
	// DefaultLimit and MaxLimit bound one window of the log.
	DefaultLimit = 50
	MaxLimit     = 200
	// MaxExport bounds one CSV export; a year of an ordinary installation
	// fits, and a narrower filter takes the rest.
	MaxExport = 50000
)

// ErrExportTooLarge is a filter that matches more than one export may hold.
var ErrExportTooLarge = errors.New("the export would hold more entries than one file may")

// AuditEntry is one entry as an administrator reads it.
type AuditEntry struct {
	ID         uuid.UUID  `json:"id"`
	Action     string     `json:"action"`
	TargetType string     `json:"targetType"`
	TargetID   *uuid.UUID `json:"targetId"`
	ActorID    *uuid.UUID `json:"actorId"`
	// ActorName is blank for the system, and for somebody no longer a member.
	ActorName string          `json:"actorName"`
	Data      json.RawMessage `json:"data"`
	IP        string          `json:"ip"`
	CreatedAt time.Time       `json:"createdAt"`
}

// AuditActor is somebody who acted, by id and the name they go by.
type AuditActor struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// AuditFacets are what the log holds to narrow it by.
type AuditFacets struct {
	Actions     []string     `json:"actions"`
	Actors      []AuditActor `json:"actors"`
	TargetTypes []string     `json:"targetTypes"`
	// RetentionDays is how long an entry is kept, 0 for ever.
	RetentionDays int `json:"retentionDays"`
}

// Filter narrows the log; every part is optional. From is inclusive, To is not.
type Filter struct {
	Action     string
	ActorID    *uuid.UUID
	TargetType string
	TargetID   *uuid.UUID
	From       *time.Time
	To         *time.Time
}

// Describe is the filter as the export's own entry keeps it.
func (f Filter) Describe() map[string]any {
	out := map[string]any{}
	if f.Action != "" {
		out["action"] = f.Action
	}
	if f.ActorID != nil {
		out["actorId"] = *f.ActorID
	}
	if f.TargetType != "" {
		out["targetType"] = f.TargetType
	}
	if f.TargetID != nil {
		out["targetId"] = *f.TargetID
	}
	if f.From != nil {
		out["from"] = f.From.UTC().Format(time.RFC3339)
	}
	if f.To != nil {
		out["to"] = f.To.UTC().Format(time.RFC3339)
	}
	return out
}

// where is the filter as SQL over audit_log a, its arguments numbered from 1.
func (f Filter) where() (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if f.Action != "" {
		add("a.action = $%d", f.Action)
	}
	if f.ActorID != nil {
		add("a.actor_user_id = $%d", *f.ActorID)
	}
	if f.TargetType != "" {
		add("a.target_type = $%d", f.TargetType)
	}
	if f.TargetID != nil {
		add("a.target_id = $%d", *f.TargetID)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at < $%d", *f.To)
	}
	if len(clauses) == 0 {
		return "true", nil
	}
	return strings.Join(clauses, " AND "), args
}

// Service reads the log for its administrators and records the acts that have
// no transaction of their own, such as an export.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

const selectRow = `
SELECT a.id, a.action, a.target_type, a.target_id, a.actor_user_id, COALESCE(u.name, ''), a.data,
       COALESCE(host(a.ip), ''), a.created_at
FROM audit_log a
LEFT JOIN app_user u ON u.id = a.actor_user_id`

func scanRow(row pgx.Row) (AuditEntry, error) {
	var r AuditEntry
	err := row.Scan(&r.ID, &r.Action, &r.TargetType, &r.TargetID, &r.ActorID, &r.ActorName, &r.Data, &r.IP, &r.CreatedAt)
	return r, err
}

// List reads one window of the log, newest first, after the cursor, and the
// cursor of the window after it, nil at the end.
func (s *Service) List(ctx context.Context, f Filter, after *keyset.Cursor, limit int) ([]AuditEntry, *string, error) {
	if limit <= 0 || limit > MaxLimit {
		limit = DefaultLimit
	}
	where, args := f.where()
	if after != nil {
		args = append(args, after.At, after.ID)
		where += fmt.Sprintf(" AND (a.created_at, a.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	out := []AuditEntry{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRow+` WHERE `+where+fmt.Sprintf(` ORDER BY a.created_at DESC, a.id DESC LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		last := out[limit-1]
		next = keyset.Next(limit, len(out), keyset.Cursor{At: last.CreatedAt, ID: last.ID})
		out = out[:limit]
	}
	return out, next, nil
}

// Facets reads which actions, people and kinds of target the log holds.
func (s *Service) Facets(ctx context.Context) (*AuditFacets, error) {
	out := &AuditFacets{Actions: []string{}, Actors: []AuditActor{}, TargetTypes: []string{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		strs := func(sql string, into *[]string) error {
			rows, err := tx.Query(ctx, sql)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var v string
				if err := rows.Scan(&v); err != nil {
					return err
				}
				*into = append(*into, v)
			}
			return rows.Err()
		}
		if err := strs(`SELECT DISTINCT action FROM audit_log ORDER BY action`, &out.Actions); err != nil {
			return err
		}
		if err := strs(`SELECT DISTINCT target_type FROM audit_log ORDER BY target_type`, &out.TargetTypes); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT a.actor_user_id, COALESCE(u.name, '')
			FROM (SELECT DISTINCT actor_user_id FROM audit_log WHERE actor_user_id IS NOT NULL) a
			LEFT JOIN app_user u ON u.id = a.actor_user_id
			ORDER BY lower(COALESCE(u.name, '')), a.actor_user_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p AuditActor
			if err := rows.Scan(&p.ID, &p.Name); err != nil {
				return err
			}
			out.Actors = append(out.Actors, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CSVHeader is the first line of an export.
var CSVHeader = []string{"time", "action", "actor_id", "actor", "target_type", "target_id", "ip", "data"}

// CSV writes the filtered log as a spreadsheet reads it, newest first, and
// refuses more than MaxExport entries: a record cut short reads as complete.
func (s *Service) CSV(ctx context.Context, f Filter) ([]byte, int, error) {
	where, args := f.where()
	args = append(args, MaxExport+1)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(CSVHeader)
	n := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRow+` WHERE `+where+fmt.Sprintf(` ORDER BY a.created_at DESC, a.id DESC LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			if n++; n > MaxExport {
				return ErrExportTooLarge
			}
			if err := w.Write(CSVLine(r)); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	w.Flush()
	return buf.Bytes(), n, w.Error()
}

// CSVLine is one entry as its line of the export.
func CSVLine(r AuditEntry) []string {
	id := func(v *uuid.UUID) string {
		if v == nil {
			return ""
		}
		return v.String()
	}
	return []string{
		r.CreatedAt.UTC().Format(time.RFC3339Nano), r.Action, id(r.ActorID), Cell(r.ActorName),
		r.TargetType, id(r.TargetID), r.IP, Cell(string(r.Data)),
	}
}

// Cell keeps a spreadsheet from reading a value as a formula, as Armature's
// export does.
func Cell(s string) string {
	if s == "" || !strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return s
	}
	return "'" + s
}

// Note records an act that changes nothing, such as a file leaving, in a
// transaction of its own; the caller hands the file over only once it succeeds.
func (s *Service) Note(ctx context.Context, e Entry) (db.LSN, error) {
	if e.Actor == uuid.Nil {
		e.Actor, _ = db.UserFrom(ctx)
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return Write(ctx, tx, e)
	})
}
