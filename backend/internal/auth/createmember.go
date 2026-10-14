package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// GeneratedPasswordLength is the length of a password made for somebody, well
// above MinPasswordLength.
const GeneratedPasswordLength = 20

// MaxMemberNameLength bounds the name an administrator gives somebody.
const MaxMemberNameLength = 200

// No look-alikes (0 O 1 l I), since an administrator reads this out or pastes
// it into a message.
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

var (
	// ErrBadMemberEmail is returned for an address that cannot be one.
	ErrBadMemberEmail = errors.New("enter an email address, such as name@example.com")
	// ErrBadMemberName is returned for a name that is too long.
	ErrBadMemberName = fmt.Errorf("a name has at most %d characters", MaxMemberNameLength)
	// ErrAlreadyMember is returned when the address already belongs here.
	ErrAlreadyMember = errors.New("that person is already a member")
)

// NewMember is a person an administrator adds. A blank Password has one made.
type NewMember struct {
	Email    string
	Name     string
	Role     OrgRole
	Password string
}

// CreatedMember says who was added. Password is set only when it was made
// here, and is the one time anybody sees it.
type CreatedMember struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   OrgRole   `json:"role"`
	// NewAccount is false when the address already had an account elsewhere,
	// which keeps its own password: the person signs in as they always did.
	NewAccount bool   `json:"newAccount"`
	Password   string `json:"password,omitempty"`
}

// CreateMember adds a person with a password to the installation. An address that already has an account
// joins with its password untouched: an administrator here must not set a password for somebody else's account.
func (s *Service) CreateMember(ctx context.Context, in NewMember, actor uuid.UUID, ip string) (*CreatedMember, db.LSN, error) {
	var err error
	email := NormalizeEmail(in.Email)
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 || len(email) > 254 || strings.ContainsAny(email, " \t\r\n<>,;") {
		return nil, 0, ErrBadMemberEmail
	}
	if !slices.Contains(AssignableRoles, in.Role) {
		return nil, 0, ErrBadRole
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	if utf8.RuneCountInString(name) > MaxMemberNameLength {
		return nil, 0, ErrBadMemberName
	}

	password, made := in.Password, false
	if password == "" {
		if password, err = generatePassword(); err != nil {
			return nil, 0, err
		}
		made = true
	} else if err := ValidatePassword(password); err != nil {
		return nil, 0, err
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return nil, 0, err
	}

	var out CreatedMember
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var userID uuid.UUID
		var existingName string
		err := tx.QueryRow(ctx, `SELECT id, name FROM app_user WHERE email = $1`, email).Scan(&userID, &existingName)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name, password_hash) VALUES ($1, $2, $3) RETURNING id`, email, name, hash).Scan(&userID); err != nil {
				return fmt.Errorf("create the account: %w", err)
			}
			out.NewAccount = true
		case err != nil:
			return err
		default:
			name = existingName
		}
		tag, err := tx.Exec(ctx, `UPDATE app_user SET role = $2 WHERE id = $1 AND role IS NULL`, userID, string(in.Role))
		if err != nil {
			return fmt.Errorf("create the membership: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrAlreadyMember
		}
		out.UserID, out.Email, out.Name, out.Role = userID, email, name, in.Role
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberCreated, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: map[string]any{"role": in.Role, "newAccount": out.NewAccount}})
	})
	if err != nil {
		return nil, 0, err
	}
	if made && out.NewAccount {
		out.Password = password
	}
	return &out, lsn, nil
}

func generatePassword() (string, error) {
	out := make([]byte, GeneratedPasswordLength)
	limit := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("make a password: %w", err)
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}
