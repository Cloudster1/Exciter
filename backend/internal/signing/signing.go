// Package signing proves who a signer is again for an electronic signature (#46): the password typed now, or a fresh
// sign-in at the identity provider, spent once. Reviews and baselines sign with it (#205).
package signing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

var (
	// ErrNeedsSession refuses a signature from anything but a signed-in session, such as a token.
	ErrNeedsSession = errors.New("a signature is given in a signed-in session")
	// ErrTooManyAttempts refuses signing after a run of wrong passwords, until it has passed.
	ErrTooManyAttempts = errors.New("too many wrong passwords")
	// ErrSpent refuses a re-authentication that is used up, expired or of another session.
	ErrSpent = errors.New("that re-authentication is used up or has expired")
)

// FieldError is a proof refused on the password field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Message }

// Passwords checks the signed-in person's own password, as auth.Service does.
type Passwords interface {
	VerifyOwnPassword(ctx context.Context, userID uuid.UUID, password string) error
}

// signer is who signs, as the request that brought them: their session, if any, and their language.
type signer struct {
	session *uuid.UUID
	lang    string
}

type signerKey struct{}

// With says which session a request rides on and in which language its reader reads, for signatures.
func With(ctx context.Context, session *uuid.UUID, lang string) context.Context {
	return context.WithValue(ctx, signerKey{}, signer{session: session, lang: lang})
}

func of(ctx context.Context) signer {
	s, _ := ctx.Value(signerKey{}).(signer)
	return s
}

// Lang is the language the signer reads the meaning in.
func Lang(ctx context.Context) string { return of(ctx).lang }

// Prove checks a password now, outside the transaction for it is slow, or that a re-authentication came; missing is
// the sentence when neither did.
func Prove(ctx context.Context, passwords Passwords, actor uuid.UUID, password string, reauthentication *uuid.UUID, missing string) error {
	if of(ctx).session == nil {
		return ErrNeedsSession
	}
	switch {
	case password != "":
		if passwords == nil {
			return &FieldError{Field: "password", Message: "Signing with a password is not set up here. Sign in again at the identity provider to sign."}
		}
		switch err := passwords.VerifyOwnPassword(ctx, actor, password); {
		case errors.Is(err, auth.ErrWrongPassword):
			return &FieldError{Field: "password", Message: "That is not your password. Type it again to sign."}
		case errors.Is(err, auth.ErrNoPassword):
			return &FieldError{Field: "password", Message: "You sign in through the identity provider, so you sign by signing in there again."}
		case errors.Is(err, auth.ErrTooManyAttempts):
			return ErrTooManyAttempts
		case err != nil:
			return err
		}
	case reauthentication == nil:
		return &FieldError{Field: "password", Message: missing}
	}
	return nil
}

// Proof is the re-authentication a signature spends, as the signature records it.
type Proof struct {
	ID              uuid.UUID
	Method          string
	AuthenticatedAt time.Time
	Session         uuid.UUID
}

// Spend finds the proof a signature stands on, in its transaction: a re-authentication recorded for a password just
// checked, or the one a sign-in at the provider gave. The signature's guard spends it.
func Spend(ctx context.Context, tx db.DBTX, actor uuid.UUID, password bool, reauthentication *uuid.UUID) (Proof, error) {
	who := of(ctx)
	if who.session == nil {
		return Proof{}, ErrNeedsSession
	}
	p := Proof{Session: *who.session}
	if password {
		if err := tx.QueryRow(ctx, `
			INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'password', now(), now() + interval '1 minute') RETURNING id, method, authenticated_at`,
			actor, p.Session).Scan(&p.ID, &p.Method, &p.AuthenticatedAt); err != nil {
			return p, fmt.Errorf("record the password given: %w", err)
		}
		return p, nil
	}
	if reauthentication == nil {
		return p, ErrSpent
	}
	err := tx.QueryRow(ctx, `
		SELECT id, method, authenticated_at FROM reauthentication
		WHERE id = $1 AND session_id = $2 AND used_at IS NULL AND expires_at > now()`, *reauthentication, p.Session).
		Scan(&p.ID, &p.Method, &p.AuthenticatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrSpent
	}
	return p, err
}

// InSession says the request rides on a signed-in session, the only thing that signs.
func InSession(ctx context.Context) bool { return of(ctx).session != nil }
