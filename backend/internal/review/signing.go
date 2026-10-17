package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/signing"
)

var (
	// ErrSignatureNeedsSession refuses a signature from anything but a signed-in session, such as a token.
	ErrSignatureNeedsSession = signing.ErrNeedsSession
	// ErrTooManyAttempts refuses signing after a run of wrong passwords, until it has passed.
	ErrTooManyAttempts = signing.ErrTooManyAttempts
	// ErrSignatureSpent refuses a re-authentication that is used up, expired or of another session.
	ErrSignatureSpent = signing.ErrSpent
)

// Passwords checks the signed-in person's own password, as auth.Service does.
type Passwords = signing.Passwords

// WithPasswords lets the service check a signer's password; without it only a re-authentication signs.
func (s *Service) WithPasswords(p Passwords) *Service {
	s.passwords = p
	return s
}

// WithSigner says which session a request rides on and in which language its reader reads, for signatures.
func WithSigner(ctx context.Context, session *uuid.UUID, lang string) context.Context {
	return signing.With(ctx, session, lang)
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

// proveSigner checks what a signature is given with before the decision is written.
func (s *Service) proveSigner(ctx context.Context, actor uuid.UUID, in DecisionInput) error {
	err := signing.Prove(ctx, s.passwords, actor, in.Password, in.Reauthentication,
		"This decision is an electronic signature. Sign with your password, or sign in again at the identity provider.")
	var field *signing.FieldError
	if errors.As(err, &field) {
		return &FieldError{Field: field.Field, Message: field.Message}
	}
	return err
}

// sign writes the signature a decision stands on, in the decision's transaction, spending the proof it was given.
func sign(ctx context.Context, tx db.DBTX, actor uuid.UUID, id uuid.UUID, h *held, in DecisionInput) error {
	proof, err := signing.Spend(ctx, tx, actor, in.Password != "", in.Reauthentication)
	if err != nil {
		return err
	}
	var key, title, hash string
	var number int
	if err := tx.QueryRow(ctx, `
		SELECT p.key, r.number, rv.title, rv.hash FROM requirement r JOIN project p ON p.id = r.project_id
		JOIN requirement_revision rv ON rv.requirement_id = r.id AND rv.number = $2
		WHERE r.id = $1`, h.requirement, h.revision).Scan(&key, &number, &title, &hash); err != nil {
		return fmt.Errorf("read what is signed: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO review_signature (review_id, requirement_id, revision, content_hash, signer_id, decision, meaning, method,
		                              authenticated_at, session_id, reauthentication_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, h.requirement, h.revision, hash, actor, in.Decision, meaning(signing.Lang(ctx), in.Decision, h.revision, requirement.Key(key, number), title),
		proof.Method, proof.AuthenticatedAt, proof.Session, proof.ID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Message == "review_signature_unproven" {
		return ErrSignatureSpent
	}
	if err != nil {
		return fmt.Errorf("record the signature: %w", err)
	}
	return nil
}
