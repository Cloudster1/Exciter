package tokenexchange

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// The partners whose outcome is kept, as partner_exchange names them.
const (
	PartnerArmature = "armature"
	PartnerStator   = "stator"
)

// SignIn is what the last sign-in's exchange came to with a partner: connected, or the sentence why not.
type SignIn struct {
	Connected bool      `json:"connected"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

// Note keeps the outcome of an exchange for the person, as the admin role: nobody claims one with SQL. It answers
// where the write landed, after the exchange's own, so the person's first reads can wait for both.
func Note(ctx context.Context, cluster *db.Cluster, partner string, user uuid.UUID, connected bool, message string) (db.LSN, error) {
	return cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO partner_exchange (partner, user_id, connected, message, at) VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (partner, user_id) DO UPDATE SET connected = EXCLUDED.connected, message = EXCLUDED.message, at = now()`,
			partner, user, connected, message)
		return err
	})
}

// Read is the person's last outcome with the partner, nil when no sign-in tried; tx reads as the person.
func Read(ctx context.Context, tx db.DBTX, partner string, user uuid.UUID) (*SignIn, error) {
	var out SignIn
	err := tx.QueryRow(ctx, `SELECT connected, message, at FROM partner_exchange WHERE partner = $1 AND user_id = $2`, partner, user).
		Scan(&out.Connected, &out.Message, &out.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
