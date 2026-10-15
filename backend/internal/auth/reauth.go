package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Limits on proving again who one is, for a signature (#46).
const (
	// MaxSignatureFailures wrong passwords within SignatureFailureWindow refuse the next attempts until it passes.
	MaxSignatureFailures   = 5
	SignatureFailureWindow = 15 * time.Minute
	// ReauthenticationTTL is how long a fresh sign-in at the identity provider stays good for the signature it was
	// asked for; the database refuses one that lasts more than ten minutes.
	ReauthenticationTTL = 5 * time.Minute
)

var (
	// ErrWrongPassword is a password that is not the signed-in person's own.
	ErrWrongPassword = errors.New("that is not your password")
	// ErrNoPassword is somebody who signs in through the identity provider, and proves who they are there.
	ErrNoPassword = errors.New("you sign in through the identity provider")
	// ErrTooManyAttempts is a run of wrong passwords, refused until it has passed.
	ErrTooManyAttempts = errors.New("too many wrong passwords")
)

// VerifyOwnPassword checks a password against the signed-in person's own, as Armature's password change does, and
// counts a wrong one; after MaxSignatureFailures within SignatureFailureWindow it refuses without looking.
func (s *Service) VerifyOwnPassword(ctx context.Context, userID uuid.UUID, password string) error {
	var (
		stored   *string
		failures int
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT password_hash, (SELECT count(*)::int FROM signature_failure WHERE user_id = $1 AND failed_at > now() - make_interval(secs => $2))
			FROM app_user WHERE id = $1 AND is_active`, userID, SignatureFailureWindow.Seconds()).Scan(&stored, &failures)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSwitchedOff
	}
	if err != nil {
		return err
	}
	if failures >= MaxSignatureFailures {
		return ErrTooManyAttempts
	}
	if stored == nil {
		return ErrNoPassword
	}
	if ok, _, err := VerifyPassword(password, *stored, s.params); err == nil && ok {
		return nil
	}
	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO signature_failure (user_id) VALUES ($1)`, userID)
		return err
	}); err != nil {
		return err
	}
	return ErrWrongPassword
}

// SignsWith says how somebody proves who they are to sign: their password, the identity provider, or neither.
func (s *Service) SignsWith(ctx context.Context, userID uuid.UUID) (string, error) {
	var password, provider bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT password_hash IS NOT NULL,
			       EXISTS (SELECT 1 FROM user_identity WHERE user_id = $1) AND EXISTS (SELECT 1 FROM oidc_provider WHERE enabled)
			FROM app_user WHERE id = $1`, userID).Scan(&password, &provider)
	})
	switch {
	case err != nil:
		return "", err
	case password:
		return SignWithPassword, nil
	case provider:
		return SignWithProvider, nil
	}
	return "", nil
}

// How somebody proves who they are to sign.
const (
	SignWithPassword = "password"
	SignWithProvider = "provider"
)
