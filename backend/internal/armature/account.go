package armature

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Account is the caller's own link to Armature.
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
	a.Connected, a.Status, a.User, a.ReadOnly, a.CheckedAt = true, row.Status, row.User, row.ReadOnly, &row.CheckedAt
	return a, nil
}

// tokenRow is the caller's stored token, still sealed.
type tokenRow struct {
	ID        uuid.UUID
	Sealed    []byte
	ReadOnly  bool
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
		row                      tokenRow
		armatureUser             *uuid.UUID
		armatureName, armatureAt *string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, token, read_only, status, armature_user_id, armature_user_name, armature_user_email, checked_at
		FROM armature_token WHERE org_id = current_org_id() AND user_id = $1`, user,
	).Scan(&row.ID, &row.Sealed, &row.ReadOnly, &row.Status, &armatureUser, &armatureName, &armatureAt, &row.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature token: %w", err)
	}
	if armatureUser != nil {
		row.User = &AccountUser{ID: *armatureUser, Name: deref(armatureName), Email: deref(armatureAt)}
	}
	return &row, nil
}

// Connect stores the caller's token once Armature accepts it as somebody in
// the connected organization.
func (s *Service) Connect(ctx context.Context, token string) (*Account, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, 0, ErrNotConnected
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) || len(token) == len(TokenPrefix) {
		return nil, 0, &FieldError{Field: "token", Message: "That is not an Armature token. Make one under Tokens in Armature and paste it here; it starts with " + TokenPrefix + "."}
	}
	var e *endpoint
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
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
		return nil, 0, &FieldError{Field: "token", Message: "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here."}
	case !e.owns(me):
		return nil, 0, &FieldError{Field: "token", Message: fmt.Sprintf("This token was made in another Armature than the one connected at %s. Make one there and paste it here.", e.BaseURL)}
	}
	sealed, err := s.seal(token, tokenContext(org.ID, user), "token")
	if err != nil {
		return nil, 0, err
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A new row rather than an update, so the row id that keys the
		// person's cache is new with the token.
		if _, err := tx.Exec(ctx, `DELETE FROM armature_token WHERE org_id = current_org_id() AND user_id = $1`, user); err != nil {
			return fmt.Errorf("forget the old Armature token: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO armature_token (org_id, user_id, token, armature_user_id, armature_user_name, armature_user_email, read_only, status, checked_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, now())`,
			user, sealed, me.User.ID, me.User.Name, me.User.Email, me.ReadOnly, StatusOK); err != nil {
			return fmt.Errorf("store the Armature token: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	s.learnOrg(ctx, org.ID, e, me)
	return out, lsn, nil
}

// Check asks Armature now whether the caller's stored token still works, and
// records what it said; ok clears a rejected mark.
func (s *Service) Check(ctx context.Context) (*Account, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	var (
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

	status, user, readOnly := StatusRejected, row.User, row.ReadOnly
	token, openErr := s.open(ctx, row, org.ID)
	if openErr == nil {
		me, err := s.me(ctx, e.BaseURL, token)
		switch {
		case errors.Is(err, ErrUnreachable):
			status = StatusUnreachable
		case err == nil && e.owns(me):
			status, user, readOnly = StatusOK, &me.User, me.ReadOnly
			s.learnOrg(ctx, org.ID, e, me)
		}
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id, name, email any
		if user != nil {
			id, name, email = user.ID, user.Name, user.Email
		}
		if _, err := tx.Exec(ctx, `
			UPDATE armature_token SET status = $2, checked_at = now(), read_only = $6,
			    armature_user_id = coalesce($3, armature_user_id),
			    armature_user_name = coalesce($4, armature_user_name),
			    armature_user_email = coalesce($5, armature_user_email)
			WHERE id = $1`, row.ID, status, id, name, email, readOnly); err != nil {
			return fmt.Errorf("record the Armature check: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	return out, lsn, err
}

// Disconnect forgets the caller's token; forgetting none is not an error.
func (s *Service) Disconnect(ctx context.Context) (db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return 0, ErrNotConnected
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM armature_token WHERE org_id = current_org_id() AND user_id = $1`, user)
		return err
	})
}

// Viewer is the caller as Armature knows them. TokenID keys their entries in
// the Cache, and Caller makes every call with their own token.
type Viewer struct {
	OrgID   uuid.UUID
	UserID  uuid.UUID
	TokenID uuid.UUID
	Caller  *Caller
	// ReadOnly is a token of Armature's read scope, which Armature refuses every write.
	ReadOnly bool
}

// Viewer returns the person ctx acts for, ready to call Armature, or the
// status saying why not: not_configured, not_connected or rejected.
func (s *Service) Viewer(ctx context.Context) (*Viewer, Status, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, "", err
	}
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, StatusNotConnected, nil
	}
	var (
		e   *endpoint
		row *tokenRow
	)
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if e, err = s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		row, err = s.ownToken(ctx, tx)
		return err
	}); err != nil {
		return nil, "", err
	}
	switch {
	case e == nil:
		return nil, StatusNotConfigured, nil
	case row == nil:
		return nil, StatusNotConnected, nil
	case row.Status == StatusRejected:
		return nil, StatusRejected, nil
	}
	token, err := s.open(ctx, row, org.ID)
	if err != nil {
		return nil, StatusRejected, nil
	}
	return &Viewer{OrgID: org.ID, UserID: user, TokenID: row.ID, Caller: s.client.As(e.BaseURL, token), ReadOnly: row.ReadOnly}, StatusOK, nil
}

// NoteRejected marks the viewer's token rejected after Armature answered it
// 401, so it is not sent again until its owner stores a new one or checks it.
func (s *Service) NoteRejected(ctx context.Context, v *Viewer) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE armature_token SET status = $2, checked_at = now() WHERE id = $1`, v.TokenID, StatusRejected)
		return err
	})
	return err
}

func (s *Service) me(ctx context.Context, base, token string) (*Me, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	me, err := s.client.As(base, token).Me(ctx)
	if errors.Is(err, ErrUnreachable) {
		s.opts.Log.Info("Armature did not answer a token check", "base_url", base, "error", err)
	}
	return me, err
}

// owns says whether a token comes from the Armature the first token taught; one that names no organization has
// only the address to go by, which already picked the installation.
func (e *endpoint) owns(me *Me) bool {
	if me == nil {
		return false
	}
	return e.ArmatureOrgID == nil || me.OrgID == uuid.Nil || *e.ArmatureOrgID == me.OrgID
}

// learnOrg keeps the Armature organization's id from the first token that
// checks out, while the address is still the one that was asked.
func (s *Service) learnOrg(ctx context.Context, org uuid.UUID, e *endpoint, me *Me) {
	if e.ArmatureOrgID != nil || me.OrgID == uuid.Nil {
		return
	}
	// As the admin role: a member's token teaches it, and exciter_app may not write it, so nobody can pin it
	// with raw SQL.
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE armature_connection SET armature_org_id = $2
			WHERE org_id = $1 AND armature_org_id IS NULL AND base_url = $3`,
			org, me.OrgID, e.BaseURL)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the Armature organization's id could not be kept", "org_id", org, "error", err)
	}
}
