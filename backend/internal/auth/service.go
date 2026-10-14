package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// lastSeenEvery is how stale last_seen_at may get before a request refreshes
// it; otherwise every request would be a write, for a field nobody reads live.
const lastSeenEvery = time.Minute

// Service implements the identity use cases. Everything here runs before a
// tenant is known, or spans tenants, so it goes through the admin path.
type Service struct {
	db         *db.Cluster
	params     PasswordParams
	sessionTTL time.Duration
	now        func() time.Time
}

// NewService constructs the identity service.
func NewService(cluster *db.Cluster, params PasswordParams, sessionTTL time.Duration) *Service {
	return &Service{db: cluster, params: params, sessionTTL: sessionTTL, now: time.Now}
}

// SessionTTL is how long a new session lasts.
func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }

// Credentials is the result of a successful sign-in: the principal, the
// session secret for the cookie, and the write's LSN for read-your-writes.
type Credentials struct {
	Principal     *Principal
	SessionSecret string
	ExpiresAt     time.Time
	LSN           db.LSN
}

// decoyHash is a real argon2id hash of an unguessable value, verified against
// when the email is unknown so that both branches of Login cost the same.
const decoyHash = "$argon2id$v=19$m=65536,t=3,p=4$c29tZS1zdGF0aWMtc2FsdA$Zm9yLXRpbWluZy1lcXVhbGl0eS1vbmx5LW5vdC1hLXNl"

// Login verifies a password and opens a session in the person's first
// organization. An unknown address and a wrong password fail alike.
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (*Credentials, error) {
	email = NormalizeEmail(email)

	var (
		p      Principal
		active bool
		stored *string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT id, email, name, COALESCE(avatar_url, ''), COALESCE(locale, ''), is_active, password_hash
			FROM app_user WHERE email = $1`, email,
		).Scan(&p.UserID, &p.Email, &p.Name, &p.AvatarURL, &p.Locale, &active, &stored)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, _, _ = VerifyPassword(password, decoyHash, s.params)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if stored == nil {
		// Somebody who signs in through a provider has no password here, and
		// saying so would tell a stranger the address has an account.
		_, _, _ = VerifyPassword(password, decoyHash, s.params)
		return nil, ErrInvalidCredentials
	}
	ok, needsRehash, err := VerifyPassword(password, *stored, s.params)
	if err != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	if !active {
		return nil, ErrSwitchedOff
	}
	if needsRehash {
		if upgraded, err := HashPassword(password, s.params); err == nil {
			_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `UPDATE app_user SET password_hash = $2 WHERE id = $1`, p.UserID, upgraded)
				return err
			})
		}
	}

	memberships, err := s.memberships(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	// Everybody signed in belongs to the installation: somebody let go hears so rather than seeing an empty shell.
	if len(memberships) == 0 {
		return nil, ErrSwitchedOff
	}
	m := memberships[0]
	orgID := &m.OrgID
	p.Org = &tenant.Org{ID: m.OrgID, Slug: m.OrgSlug}
	p.OrgName = m.OrgName
	p.Role = m.Role

	expiresAt := s.now().Add(s.sessionTTL)
	var secret string
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, sessionSecret, err := OpenSession(ctx, tx, p.UserID, orgID, ProofPassword, expiresAt, userAgent, ip)
		if err != nil {
			return err
		}
		secret = sessionSecret
		p.SessionID = &id
		p.Proof = ProofPassword
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Credentials{Principal: &p, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}

// sessionPrincipalSQL resolves a session to its caller and standing in one
// round trip; outer joins, so a session whose membership went is told apart from none.
const sessionPrincipalSQL = `
SELECT s.id, s.last_seen_at, s.proof,
       u.id, u.email, u.name, COALESCE(u.avatar_url, ''), COALESCE(u.locale, ''), u.is_active,
       o.id, o.slug, o.name, m.org_role
FROM user_session s
JOIN app_user u ON u.id = s.user_id
LEFT JOIN org o ON o.id = s.current_org_id AND o.archived_at IS NULL
LEFT JOIN org_member m ON m.org_id = o.id AND m.user_id = u.id
WHERE s.token_hash = $1 AND s.expires_at > now()`

// Authenticate resolves a session secret to its principal.
func (s *Service) Authenticate(ctx context.Context, secret string) (*Principal, error) {
	if secret == "" {
		return nil, ErrInvalidToken
	}
	if IsAPIToken(secret) {
		return s.authenticateAPIToken(ctx, secret)
	}
	var (
		p          Principal
		sessionID  uuid.UUID
		lastSeen   time.Time
		active     bool
		orgID      *uuid.UUID
		orgSlug    *string
		orgName    *string
		memberRole *string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, sessionPrincipalSQL, HashToken(secret)).Scan(
			&sessionID, &lastSeen, &p.Proof,
			&p.UserID, &p.Email, &p.Name, &p.AvatarURL, &p.Locale, &active,
			&orgID, &orgSlug, &orgName, &memberRole,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrSwitchedOff
	}
	p.SessionID = &sessionID
	// A membership row is required, not merely an org row: letting somebody go ends their open session at once,
	// so the browser goes back to sign in and hears why there.
	if orgID == nil || memberRole == nil {
		if err := s.Logout(ctx, sessionID); err != nil {
			return nil, err
		}
		return nil, ErrInvalidToken
	}
	p.Org = &tenant.Org{ID: *orgID, Slug: *orgSlug}
	p.OrgName = *orgName
	p.Role = OrgRole(*memberRole)
	if s.now().Sub(lastSeen) > lastSeenEvery {
		_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE user_session SET last_seen_at = now() WHERE id = $1`, sessionID)
			return err
		})
	}
	return &p, nil
}

// Logout ends a session. Ending one that is already gone is not an error.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM user_session WHERE id = $1`, sessionID)
		return err
	})
	return err
}

// memberships are the person's, oldest first, so the first is a stable default while the integration suite still
// lets the same people in to organizations of its own (#90).
func (s *Service) memberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	out := []Membership{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT o.id, o.slug, o.name, m.org_role
			FROM org_member m
			JOIN org o ON o.id = m.org_id
			WHERE m.user_id = $1 AND o.archived_at IS NULL
			ORDER BY m.created_at, o.slug`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Membership
			if err := rows.Scan(&m.OrgID, &m.OrgSlug, &m.OrgName, &m.Role); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// SetLocale records the language a person reads the interface in;
// LocaleBrowser hands the choice back to their browser.
func (s *Service) SetLocale(ctx context.Context, userID uuid.UUID, locale Locale) (db.LSN, error) {
	if !slices.Contains(Locales, locale) {
		return 0, ErrBadLocale
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET locale = NULLIF($2, '') WHERE id = $1`, userID, string(locale))
		return err
	})
}

// InstallationOrg is the organization a sign-in that names none is for, such as single sign-on's: the oldest,
// as the seed finds it. Through the admin path, since nobody is signed in yet.
func (s *Service) InstallationOrg(ctx context.Context) (*tenant.Org, error) {
	var org tenant.Org
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id, slug FROM org WHERE archived_at IS NULL ORDER BY created_at, slug LIMIT 1`).Scan(&org.ID, &org.Slug)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoInstallation
	}
	if err != nil {
		return nil, fmt.Errorf("find the installation: %w", err)
	}
	return &org, nil
}

// AccountID finds the account of an address, for a bootstrap acting in that
// person's name; ErrNoSuchMember when there is none.
func (s *Service) AccountID(ctx context.Context, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, NormalizeEmail(email)).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoSuchMember
	}
	return id, err
}

// EnsureAdmin makes email a local owner of ctx's organization, reporting whether
// the account is new. Run again it changes nothing, or resets a changed password.
func (s *Service) EnsureAdmin(ctx context.Context, email, name, password string) (bool, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return false, err
	}
	email = NormalizeEmail(email)
	if !strings.Contains(email, "@") {
		return false, fmt.Errorf("%q is not an email address", email)
	}
	if err := ValidatePassword(password); err != nil {
		return false, err
	}
	if strings.TrimSpace(name) == "" {
		name, _, _ = strings.Cut(email, "@")
	}

	var stored *string
	err = s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE email = $1`, email).Scan(&stored)
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	var hash *string
	if stored == nil {
		h, err := HashPassword(password, s.params)
		if err != nil {
			return false, err
		}
		hash = &h
	} else if ok, _, _ := VerifyPassword(password, *stored, s.params); !ok {
		h, err := HashPassword(password, s.params)
		if err != nil {
			return false, err
		}
		hash = &h
	}

	var created bool
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		orgID := org.ID
		var userID uuid.UUID
		// xmax is zero on a row this statement inserted rather than updated.
		err := tx.QueryRow(ctx, `
			INSERT INTO app_user (email, name, password_hash) VALUES ($1, $2, $3)
			ON CONFLICT (email) DO UPDATE SET password_hash = COALESCE($3, app_user.password_hash)
			RETURNING id, xmax = 0`, email, name, hash).Scan(&userID, &created)
		if err != nil {
			return fmt.Errorf("create the administrator: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')
			ON CONFLICT (user_id) DO UPDATE SET org_role = 'owner', role_source = 'manual'
			WHERE org_member.org_role <> 'owner'`, orgID, userID)
		return err
	})
	return created, err
}

// OpenSession creates a session row and returns its id and secret. The secret
// exists only in this return value and the cookie; the row holds its digest.
func OpenSession(ctx context.Context, tx db.DBTX, userID uuid.UUID, orgID *uuid.UUID, proof Proof, expiresAt time.Time, userAgent, ip string) (uuid.UUID, string, error) {
	secret, digest, err := GenerateToken()
	if err != nil {
		return uuid.Nil, "", err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO user_session (user_id, token_hash, current_org_id, proof, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, '')::inet, $7)
		RETURNING id`,
		userID, digest, orgID, string(proof), userAgent, ip, expiresAt,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("create session: %w", err)
	}
	return id, secret, nil
}

// NormalizeEmail lowercases and trims an address. The column is citext, so the
// database is case insensitive anyway; this keeps what is stored tidy.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
