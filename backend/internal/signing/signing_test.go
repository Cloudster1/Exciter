package signing

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// checked answers every password with err, as auth.Service would.
type checked struct{ err error }

func (c checked) VerifyOwnPassword(context.Context, uuid.UUID, string) error { return c.err }

const missing = "Sign with your password, or sign in again at the identity provider."

// A proof is refused outside a session, and on the password field with the sentence each failure has.
func TestAProofNeedsASessionAndTheRightPassword(t *testing.T) {
	session := uuid.New()
	signedIn := With(context.Background(), &session, "en")
	reauthentication := uuid.New()
	broken := errors.New("the database is gone")
	for _, c := range []struct {
		name      string
		ctx       context.Context
		passwords Passwords
		password  string
		reauth    *uuid.UUID
		field     string
		err       error
	}{
		{name: "no session", ctx: With(context.Background(), nil, "en"), passwords: checked{}, password: "secret", err: ErrNeedsSession},
		{name: "no session in the context at all", ctx: context.Background(), passwords: checked{}, reauth: &reauthentication, err: ErrNeedsSession},
		{name: "a password where none is checked", ctx: signedIn, password: "secret",
			field: "Signing with a password is not set up here. Sign in again at the identity provider to sign."},
		{name: "a wrong password", ctx: signedIn, passwords: checked{auth.ErrWrongPassword}, password: "wrong",
			field: "That is not your password. Type it again to sign."},
		{name: "a person without a password", ctx: signedIn, passwords: checked{auth.ErrNoPassword}, password: "secret",
			field: "You sign in through the identity provider, so you sign by signing in there again."},
		{name: "too many wrong passwords", ctx: signedIn, passwords: checked{auth.ErrTooManyAttempts}, password: "secret", err: ErrTooManyAttempts},
		{name: "a check that fails", ctx: signedIn, passwords: checked{broken}, password: "secret", err: broken},
		{name: "neither a password nor a re-authentication", ctx: signedIn, passwords: checked{}, field: missing},
		{name: "the right password", ctx: signedIn, passwords: checked{}, password: "secret"},
		{name: "a re-authentication", ctx: signedIn, reauth: &reauthentication},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Prove(c.ctx, c.passwords, uuid.New(), c.password, c.reauth, missing)
			var field *FieldError
			switch {
			case c.field != "":
				if !errors.As(err, &field) || field.Field != "password" || field.Message != c.field {
					t.Errorf("got %v, want %q on password", err, c.field)
				}
			case !errors.Is(err, c.err):
				t.Errorf("got %v, want %v", err, c.err)
			}
		})
	}
}

func TestTheSignersLanguageAndSessionRideOnTheContext(t *testing.T) {
	session := uuid.New()
	ctx := With(context.Background(), &session, "de")
	if Lang(ctx) != "de" || !InSession(ctx) {
		t.Errorf("the context reads language %q, in a session %v", Lang(ctx), InSession(ctx))
	}
	if Lang(context.Background()) != "" || InSession(context.Background()) {
		t.Error("a bare context names a language or a session")
	}
}
