package requirement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// MaxLockReasonLength bounds why an administrator locked a requirement by hand (#205).
const MaxLockReasonLength = 500

// LockBy says what holds a requirement locked.
type LockBy string

const (
	LockByStatus LockBy = "status"
	LockByHand   LockBy = "hand"
)

// LockBys lists every LockBy, for the API document.
var LockBys = []LockBy{LockByStatus, LockByHand}

// Lock says why a requirement takes no new revision, move, link or trash; a hand lock is answered over a status one.
type Lock struct {
	By           LockBy     `json:"by"`
	Status       *StatusRef `json:"status"`
	Reason       string     `json:"reason"`
	LockedByName string     `json:"lockedByName"`
	LockedAt     *time.Time `json:"lockedAt"`
}

// LockInput locks a requirement by hand, or changes the reason of a hand lock already there.
type LockInput struct {
	Reason string `json:"reason"`
}

// lockSQL reads what locks the requirement aliased r, whose status is joined as st; scannedLock takes it.
const lockSQL = `, COALESCE(st.locks, false), r.locked_at, r.lock_reason,
       COALESCE((SELECT u.name FROM app_user u WHERE u.id = r.locked_by), '')`

type scannedLock struct {
	byStatus bool
	at       *time.Time
	reason   string
	byName   string
}

func (s *scannedLock) targets() []any { return []any{&s.byStatus, &s.at, &s.reason, &s.byName} }

// lock is what the scan read, nil when nothing locks the requirement.
func (s *scannedLock) lock(status *StatusRef) *Lock {
	switch {
	case s.at != nil:
		return &Lock{By: LockByHand, Status: status, Reason: s.reason, LockedByName: s.byName, LockedAt: s.at}
	case s.byStatus:
		return &Lock{By: LockByStatus, Status: status}
	}
	return nil
}

// locked takes from what a requirement offers what its lock refuses.
func (c Can) locked(l *Lock) Can {
	if l != nil {
		c.Edit, c.Delete, c.Link = false, false, false
	}
	return c
}

var (
	// ErrSectionNotLocked is a hand lock asked of a section or of a requirement in the trash.
	ErrSectionNotLocked = errors.New("a section is not locked")
	// ErrNotLockedByHand is an unlock of a requirement no administrator locked.
	ErrNotLockedByHand = errors.New("the requirement is not locked by hand")
	// ErrNotLockKeeper is a hand lock or unlock by somebody but the project's administrators.
	ErrNotLockKeeper = errors.New("only a project's administrators lock its requirements by hand")
)

// lockRefusal names what requirement_lock and requirement_unlock refused.
func lockRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "requirement_lock_forbidden":
			return ErrNotLockKeeper
		case "requirement_lock_section":
			return ErrSectionNotLocked
		}
	}
	return err
}

// Lock locks a requirement by hand with a reason, or changes the reason; the project's administrators'.
func (s *Service) Lock(ctx context.Context, actor uuid.UUID, key string, in LockInput) (*Requirement, db.LSN, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > MaxLockReasonLength {
		return nil, 0, &FieldError{Field: "reason", Message: fmt.Sprintf("Say in at most %d characters why it is locked, such as Released in 2.0.", MaxLockReasonLength)}
	}
	return s.setLock(ctx, actor, key, func(ctx context.Context, tx db.DBTX, l *loaded) error {
		if l.Kind == KindSection || l.TrashedAt != nil {
			return ErrSectionNotLocked
		}
		if _, err := tx.Exec(ctx, `SELECT requirement_lock($1, $2)`, l.ID, reason); err != nil {
			return lockRefusal(err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRequirementLocked, TargetType: "requirement", TargetID: &l.ID,
			Actor: actor, Data: map[string]any{"requirement": l.Key, "reason": reason}})
	})
}

// Unlock clears a requirement's hand lock; a lock by its status stays. The project's administrators'.
func (s *Service) Unlock(ctx context.Context, actor uuid.UUID, key string) (*Requirement, db.LSN, error) {
	return s.setLock(ctx, actor, key, func(ctx context.Context, tx db.DBTX, l *loaded) error {
		var cleared bool
		if err := tx.QueryRow(ctx, `SELECT requirement_unlock($1)`, l.ID).Scan(&cleared); err != nil {
			return lockRefusal(err)
		}
		if !cleared {
			return ErrNotLockedByHand
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRequirementUnlocked, TargetType: "requirement", TargetID: &l.ID,
			Actor: actor, Data: map[string]any{"requirement": l.Key}})
	})
}

// setLock reads the requirement for an administrator of its open project, changes its hand lock and answers it as it
// is then.
func (s *Service) setLock(ctx context.Context, actor uuid.UUID, key string, change func(context.Context, db.DBTX, *loaded) error) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		var administer bool
		if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer')`, l.projectID, actor).Scan(&administer); err != nil {
			return err
		}
		switch {
		case l.archived:
			return project.ErrArchived
		case !administer:
			return ErrNotLockKeeper
		}
		if err := change(ctx, tx, l); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": l.ID, "field": ChangeLock}); err != nil {
			return err
		}
		now, err := findByID(ctx, tx, actor, l.ID)
		if err == nil {
			out = &now.Requirement
		}
		return err
	})
	return out, lsn, err
}

// LockedSentence is the database's sentence when it refused a change because a requirement is locked.
func LockedSentence(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "requirement_locked" {
		return pgErr.Message, true
	}
	return "", false
}
