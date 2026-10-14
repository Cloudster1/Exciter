package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// ErrSignedOut refuses a caller nobody signed in as, who was told nothing.
var ErrSignedOut = errors.New("notifications need somebody signed in")

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// On says whether a switch map has a kind on; a kind it leaves out is on.
func On(switches map[Kind]bool, kind Kind) bool {
	v, set := switches[kind]
	return !set || v
}

// every names each kind with its switch, a kind the map leaves out being on.
func every(switches map[Kind]bool) map[Kind]bool {
	out := make(map[Kind]bool, len(Kinds))
	for _, k := range Kinds {
		out[k] = On(switches, k)
	}
	return out
}

// DefaultPreferences is what having saved nothing means: every kind on, mail at once, and watching what one writes.
func DefaultPreferences() Preferences {
	return Preferences{InApp: every(nil), Email: every(nil), Digest: DigestOff, AutoWatch: true}
}

func kindList() string {
	names := make([]string, len(Kinds))
	for i, k := range Kinds {
		names[i] = string(k)
	}
	return strings.Join(names, ", ")
}

// Validate refuses a digest or a kind the product does not know.
func (p Preferences) Validate() error {
	for field, switches := range map[string]map[Kind]bool{"inApp": p.InApp, "email": p.Email} {
		for k := range switches {
			if !slices.Contains(Kinds, k) {
				return &FieldError{Field: field, Message: fmt.Sprintf("%q is not a kind of notification. Choose from %s.", k, kindList())}
			}
		}
	}
	if !slices.Contains(Digests, p.Digest) {
		return &FieldError{Field: "digest", Message: "That is not a digest schedule. Choose off, hourly or daily."}
	}
	return nil
}

func readPreferences(ctx context.Context, tx db.DBTX, userID uuid.UUID) (Preferences, error) {
	var (
		inApp, email map[Kind]bool
		p            Preferences
	)
	err := tx.QueryRow(ctx, `SELECT in_app, email, digest, auto_watch FROM notification_preference WHERE user_id = $1`, userID).
		Scan(&inApp, &email, &p.Digest, &p.AutoWatch)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultPreferences(), nil
	}
	if err != nil {
		return p, err
	}
	p.InApp, p.Email = every(inApp), every(email)
	return p, nil
}

// Excerpt cuts words a notification quotes to MaxExcerptLength characters, on one line.
func Excerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= MaxExcerptLength {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:MaxExcerptLength]))
}

// Service reads and marks a person's notifications and keeps their preferences.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

func actorOf(ctx context.Context) (uuid.UUID, error) {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return uuid.Nil, ErrSignedOut
	}
	return actor, nil
}

// selectNotifications reads $1's own rows with the requirement as it is now, about requirements they may still
// view, out of the trash.
const selectNotifications = `
SELECT n.id, n.kind, COALESCE(u.name, ''), r.id, p.key, r.number, r.kind, r.title, r.revision, st.id,
       COALESCE(st.name, ''), COALESCE(st.category, ''), n.revision, n.thread_id, n.comment_id, n.review_id, n.link_id,
       n.excerpt, n.created_at, n.read_at
FROM notification n
JOIN requirement r ON r.id = n.requirement_id
JOIN project p ON p.id = r.project_id
LEFT JOIN req_status st ON st.id = r.status_id
LEFT JOIN app_user u ON u.id = n.actor_id
WHERE n.user_id = $1 AND r.trashed_at IS NULL AND perm_requirement_viewable(r.id, $1::uuid)`

func scanNotifications(rows pgx.Rows) ([]Notification, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
		var (
			n        Notification
			ref      requirement.Ref
			number   int
			statusID *uuid.UUID
			status   requirement.StatusRef
		)
		if err := row.Scan(&n.ID, &n.Kind, &n.ActorName, &ref.ID, &ref.ProjectKey, &number, &ref.Kind, &ref.Title, &ref.Revision,
			&statusID, &status.Name, &status.Category, &n.Revision, &n.ThreadID, &n.CommentID, &n.ReviewID, &n.LinkID,
			&n.Excerpt, &n.CreatedAt, &n.ReadAt); err != nil {
			return n, err
		}
		ref.Key = requirement.Key(ref.ProjectKey, number)
		if statusID != nil {
			status.ID = *statusID
			ref.Status = &status
		}
		n.Requirement = &ref
		return n, nil
	})
}

// List is what the caller was told, the latest first, and how many the filter lists in all.
func (s *Service) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]Notification, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	filter := ""
	if unreadOnly {
		filter = ` AND n.read_at IS NULL`
	}
	out := []Notification{}
	var total int
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (`+selectNotifications+filter+`) listed`, actor).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectNotifications+filter+` ORDER BY n.created_at DESC, n.id DESC LIMIT $2 OFFSET $3`, actor, limit, offset)
		if err != nil {
			return err
		}
		out, err = scanNotifications(rows)
		return err
	})
	return out, total, err
}

// Unread counts what the caller has not read, for the bell.
func (s *Service) Unread(ctx context.Context) (int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM (`+selectNotifications+` AND n.read_at IS NULL) unread`, actor).Scan(&n)
	})
	return n, err
}

// MarkRead marks the named rows read, or all of them; rows that are not the caller's are left alone.
func (s *Service) MarkRead(ctx context.Context, in ReadInput) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	if !in.All && len(in.IDs) == 0 {
		return 0, &FieldError{Field: "ids", Message: "Name the notifications to mark read, or send all as true."}
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if in.All {
			_, err := tx.Exec(ctx, `UPDATE notification SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, actor)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE notification SET read_at = now() WHERE user_id = $1 AND read_at IS NULL AND id = ANY($2)`, actor, in.IDs)
		return err
	})
}

// Preferences are the caller's, or the defaults when they saved none.
func (s *Service) Preferences(ctx context.Context) (Preferences, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return Preferences{}, err
	}
	var out Preferences
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readPreferences(ctx, tx, actor)
		return err
	})
	return out, err
}

// SavePreferences replaces the caller's preferences with the ones sent, a kind left out being on.
func (s *Service) SavePreferences(ctx context.Context, in Preferences) (Preferences, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return Preferences{}, 0, err
	}
	if err := in.Validate(); err != nil {
		return Preferences{}, 0, err
	}
	inApp, err := json.Marshal(every(in.InApp))
	if err != nil {
		return Preferences{}, 0, err
	}
	email, err := json.Marshal(every(in.Email))
	if err != nil {
		return Preferences{}, 0, err
	}
	var out Preferences
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_preference (user_id, in_app, email, digest, auto_watch)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id) DO UPDATE
			SET in_app = EXCLUDED.in_app, email = EXCLUDED.email, digest = EXCLUDED.digest,
			    auto_watch = EXCLUDED.auto_watch, updated_at = now()`,
			actor, inApp, email, in.Digest, in.AutoWatch); err != nil {
			return err
		}
		var err error
		out, err = readPreferences(ctx, tx, actor)
		return err
	})
	return out, lsn, err
}
