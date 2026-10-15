package stator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Account is the caller's own link to Stator.
func (s *Service) Account(ctx context.Context) (*Account, error) {
	var out *Account
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.account(ctx, tx)
		return err
	})
	return out, err
}

func (s *Service) account(ctx context.Context, tx db.DBTX) (*Account, error) {
	e, err := s.endpoint(ctx, tx)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return &Account{Status: StatusNotConfigured}, nil
	}
	a := &Account{Configured: true, BaseURL: e.BaseURL, Status: StatusNotConnected}
	row, err := s.ownToken(ctx, tx)
	if err != nil || row == nil {
		return a, err
	}
	a.Connected, a.Status, a.User, a.CheckedAt = true, row.Status, row.User, &row.CheckedAt
	return a, nil
}

// tokenRow is the caller's stored token, still sealed.
type tokenRow struct {
	ID        uuid.UUID
	Sealed    []byte
	Status    Status
	User      *AccountUser
	CheckedAt time.Time
}

func (s *Service) ownToken(ctx context.Context, tx db.DBTX) (*tokenRow, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, ErrNotConnected
	}
	var (
		row                  tokenRow
		statorUser           *uuid.UUID
		statorName, statorAt *string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, token, status, stator_user_id, stator_user_name, stator_user_email, checked_at
		FROM stator_token WHERE user_id = $1`, user,
	).Scan(&row.ID, &row.Sealed, &row.Status, &statorUser, &statorName, &statorAt, &row.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Stator token: %w", err)
	}
	if statorUser != nil {
		row.User = &AccountUser{ID: *statorUser, Name: deref(statorName), Email: deref(statorAt)}
	}
	return &row, nil
}

// Connect stores the caller's token once the connected Stator accepts it.
func (s *Service) Connect(ctx context.Context, token string) (*Account, db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, 0, ErrNotConnected
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) || len(token) == len(TokenPrefix) {
		return nil, 0, &FieldError{Field: "token", Message: "That is not a Stator token. Make one under Your account, Access tokens in Stator and paste it here; it starts with " + TokenPrefix + "."}
	}
	var e *endpoint
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		e, err = s.endpoint(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	if e == nil {
		return nil, 0, ErrNotConfigured
	}

	me, err := s.me(ctx, e.BaseURL, token)
	switch {
	case errors.Is(err, ErrUnreachable):
		return nil, 0, err
	case err != nil:
		return nil, 0, &FieldError{Field: "token", Message: "Stator did not accept this token. Make a new one under Your account, Access tokens in Stator and paste it here."}
	}
	sealed, err := s.seal(token, tokenContext(e.Installation, user), "token")
	if err != nil {
		return nil, 0, err
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A new row rather than an update, so the row id that keys the person's cache is new with the token.
		if _, err := tx.Exec(ctx, `DELETE FROM stator_token WHERE user_id = $1`, user); err != nil {
			return fmt.Errorf("forget the old Stator token: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO stator_token (user_id, token, stator_user_id, stator_user_name, stator_user_email, status, checked_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())`,
			user, sealed, me.User.ID, me.User.Name, me.User.Email, StatusOK); err != nil {
			return fmt.Errorf("store the Stator token: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// Check asks Stator now whether the caller's stored token still works, and records what it said; ok clears a
// rejected mark. What Stator offers is read again on the way.
func (s *Service) Check(ctx context.Context) (*Account, db.LSN, error) {
	var (
		err error
		e   *endpoint
		row *tokenRow
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if e, err = s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		row, err = s.ownToken(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	if e == nil || row == nil {
		out, err := s.Account(db.PinPrimary(ctx))
		return out, 0, err
	}

	var noted db.LSN
	if meta, err := s.describe(ctx, e.BaseURL); err == nil {
		noted = s.noteOffers(ctx, e.BaseURL, meta)
	}
	status, user := StatusRejected, row.User
	token, openErr := s.open(ctx, row, e.Installation)
	if openErr == nil {
		me, err := s.me(ctx, e.BaseURL, token)
		switch {
		case errors.Is(err, ErrUnreachable):
			status = StatusUnreachable
		case err == nil:
			status, user = StatusOK, &me.User
		}
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id, name, email any
		if user != nil {
			id, name, email = user.ID, user.Name, user.Email
		}
		if _, err := tx.Exec(ctx, `
			UPDATE stator_token SET status = $2, checked_at = now(),
			    stator_user_id = coalesce($3, stator_user_id),
			    stator_user_name = coalesce($4, stator_user_name),
			    stator_user_email = coalesce($5, stator_user_email)
			WHERE id = $1`, row.ID, status, id, name, email); err != nil {
			return fmt.Errorf("record the Stator check: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	return out, max(lsn, noted), err
}

// Disconnect forgets the caller's token; forgetting none is not an error.
func (s *Service) Disconnect(ctx context.Context) (db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return 0, ErrNotConnected
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM stator_token WHERE user_id = $1`, user)
		return err
	})
}

func (s *Service) me(ctx context.Context, base, token string) (*Me, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	me, err := s.client.As(base, token).Me(ctx)
	if errors.Is(err, ErrUnreachable) {
		s.opts.Log.Info("Stator did not answer a token check", "base_url", base, "error", err)
	}
	return me, err
}
