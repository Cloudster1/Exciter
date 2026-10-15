package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

var (
	// ErrSignatureNeedsSession refuses a signature from anything but a signed-in session, such as a token.
	ErrSignatureNeedsSession = errors.New("a signature is given in a signed-in session")
	// ErrTooManyAttempts refuses signing after a run of wrong passwords, until it has passed.
	ErrTooManyAttempts = errors.New("too many wrong passwords")
	// ErrSignatureSpent refuses a re-authentication that is used up, expired or of another session.
	ErrSignatureSpent = errors.New("that re-authentication is used up or has expired")
)

// Passwords checks the signed-in person's own password, as auth.Service does.
type Passwords interface {
	VerifyOwnPassword(ctx context.Context, userID uuid.UUID, password string) error
}

// WithPasswords lets the service check a signer's password; without it only a re-authentication signs.
func (s *Service) WithPasswords(p Passwords) *Service {
	s.passwords = p
	return s
}

// signer is who decides, as the request that brought them: their session, if any, and their language.
type signer struct {
	session *uuid.UUID
	lang    string
}

type signerKey struct{}

// WithSigner says which session a request rides on and in which language its reader reads, for signatures.
func WithSigner(ctx context.Context, session *uuid.UUID, lang string) context.Context {
	return context.WithValue(ctx, signerKey{}, signer{session: session, lang: lang})
}

func signerOf(ctx context.Context) signer {
	s, _ := ctx.Value(signerKey{}).(signer)
	return s
}

// meaning is the sentence a signature means, which the signer confirms and the signature keeps.
func meaning(lang string, d Decision, revision int, key, title string) string {
	switch {
	case lang == "de" && d == DecisionApprove:
		return fmt.Sprintf("Ich genehmige Revision %d von %s, %s, als eine der genehmigenden Personen.", revision, key, title)
	case lang == "de":
		return fmt.Sprintf("Ich lehne Revision %d von %s, %s, als eine der genehmigenden Personen ab.", revision, key, title)
	case d == DecisionApprove:
		return fmt.Sprintf("I approve revision %d of %s, %s, as one of its approvers.", revision, key, title)
	}
	return fmt.Sprintf("I reject revision %d of %s, %s, as one of its approvers.", revision, key, title)
}

// signed says whether deciding on the review is a signature, read as the actor sees the review.
func (s *Service) signed(ctx context.Context, id uuid.UUID) (bool, error) {
	var signed bool
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT COALESCE((p.settings ->> 'signedApprovals')::boolean, false)
			FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id JOIN project p ON p.id = r.project_id
			WHERE v.id = $1`, id).Scan(&signed)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return signed, err
}

// proveSigner checks what a signature is given with before the decision is written: a password now, or a
// re-authentication the provider gave. A password is checked here, outside the transaction, for it is slow.
func (s *Service) proveSigner(ctx context.Context, actor uuid.UUID, in DecisionInput) error {
	who := signerOf(ctx)
	if who.session == nil {
		return ErrSignatureNeedsSession
	}
	switch {
	case in.Password != "":
		if s.passwords == nil {
			return &FieldError{Field: "password", Message: "Signing with a password is not set up here. Sign in again at the identity provider to sign."}
		}
		switch err := s.passwords.VerifyOwnPassword(ctx, actor, in.Password); {
		case errors.Is(err, auth.ErrWrongPassword):
			return &FieldError{Field: "password", Message: "That is not your password. Type it again to sign."}
		case errors.Is(err, auth.ErrNoPassword):
			return &FieldError{Field: "password", Message: "You sign in through the identity provider, so you sign by signing in there again."}
		case errors.Is(err, auth.ErrTooManyAttempts):
			return ErrTooManyAttempts
		case err != nil:
			return err
		}
	case in.Reauthentication == nil:
		return &FieldError{Field: "password", Message: "This decision is an electronic signature. Sign with your password, or sign in again at the identity provider."}
	}
	return nil
}

// sign writes the signature a decision stands on, in the decision's transaction: a password just checked spends a
// re-authentication made for it, a sign-in at the provider spends the one it gave.
func sign(ctx context.Context, tx db.DBTX, actor uuid.UUID, id uuid.UUID, h *held, in DecisionInput) error {
	who := signerOf(ctx)
	var (
		proof  uuid.UUID
		method string
		at     time.Time
	)
	if in.Password != "" {
		if err := tx.QueryRow(ctx, `
			INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'password', now(), now() + interval '1 minute') RETURNING id, method, authenticated_at`,
			actor, *who.session).Scan(&proof, &method, &at); err != nil {
			return fmt.Errorf("record the password given: %w", err)
		}
	} else {
		err := tx.QueryRow(ctx, `
			SELECT id, method, authenticated_at FROM reauthentication
			WHERE id = $1 AND session_id = $2 AND used_at IS NULL AND expires_at > now()`, *in.Reauthentication, *who.session).Scan(&proof, &method, &at)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSignatureSpent
		}
		if err != nil {
			return err
		}
	}
	var key, title, hash string
	var number int
	if err := tx.QueryRow(ctx, `
		SELECT p.key, r.number, rv.title, rv.hash FROM requirement r JOIN project p ON p.id = r.project_id
		JOIN requirement_revision rv ON rv.requirement_id = r.id AND rv.number = $2
		WHERE r.id = $1`, h.requirement, h.revision).Scan(&key, &number, &title, &hash); err != nil {
		return fmt.Errorf("read what is signed: %w", err)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO review_signature (review_id, requirement_id, revision, content_hash, signer_id, decision, meaning, method,
		                              authenticated_at, session_id, reauthentication_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, h.requirement, h.revision, hash, actor, in.Decision, meaning(who.lang, in.Decision, h.revision, requirement.Key(key, number), title),
		method, at, *who.session, proof)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Message == "review_signature_unproven" {
		return ErrSignatureSpent
	}
	if err != nil {
		return fmt.Errorf("record the signature: %w", err)
	}
	return nil
}
