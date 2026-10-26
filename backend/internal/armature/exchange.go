package armature

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
)

// sourceExchange marks a token row a sign-in's exchange stored, as armature_token.source names it.
const sourceExchange = "exchange"

// partnerName is Armature as the sentences of the exchange name it.
const partnerName = "Armature"

// partnerApp is Exciter's app at the connected Armature, read as the admin role: its secret is no member's.
type partnerApp struct {
	tokenexchange.App
	Installation uuid.UUID
	Offered      Offered
	ArmatureOrg  *uuid.UUID
}

func (s *Service) partnerApp(ctx context.Context, tx db.DBTX) (*partnerApp, error) {
	var (
		app    partnerApp
		id     *string
		sealed []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT (SELECT id FROM installation), base_url, capabilities, armature_org_id, client_id, client_secret
		FROM armature_connection`).Scan(&app.Installation, &app.BaseURL, &app.Offered, &app.ArmatureOrg, &id, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature connection: %w", err)
	}
	if id != nil && sealed != nil {
		plain, err := s.box.Open(sealed, clientContext(app.Installation))
		if err != nil {
			s.opts.Log.Warn("Exciter's client secret at Armature could not be opened", "error", err)
		} else {
			app.ClientID, app.ClientSecret = *id, string(plain)
		}
	}
	return &app, nil
}

// ExchangeAtSignIn connects the person who just signed in through the identity provider, by trading subject, their
// access token there, for their tokens at Armature. It never fails the sign-in: what it came to is kept for the
// account page, in a sentence when it did not connect them. A token the person stored themselves and that still
// works is left as it is. It answers where its last write landed, for the person's first reads.
func (s *Service) ExchangeAtSignIn(ctx context.Context, user uuid.UUID, subject string) db.LSN {
	if !s.opts.TokenExchange || subject == "" {
		return 0
	}
	exchangeCtx, cancel := context.WithTimeout(ctx, tokenexchange.SignInTimeout)
	defer cancel()
	connected, why := s.exchange(exchangeCtx, user, subject)
	if !connected && why == "" {
		return 0
	}
	// An exchange that ran out of time has spent exchangeCtx, and that is the outcome the account page most needs.
	noteCtx, cancelNote := context.WithTimeout(context.WithoutCancel(ctx), tokenexchange.NoteTimeout)
	defer cancelNote()
	lsn, err := tokenexchange.Note(noteCtx, s.db, tokenexchange.PartnerArmature, user, connected, why)
	if err != nil {
		s.opts.Log.Warn("what the sign-in's exchange at Armature came to could not be kept", "user_id", user, "error", err)
	}
	return lsn
}

// exchange is ExchangeAtSignIn; why is empty when there was nothing to try, such as no Armature connected.
func (s *Service) exchange(ctx context.Context, user uuid.UUID, subject string) (connected bool, why string) {
	var (
		app  *partnerApp
		kept bool
	)
	err := s.db.ReadAdmin(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		var err error
		if app, err = s.partnerApp(ctx, tx); err != nil || app == nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM armature_token WHERE user_id = $1 AND source = 'token' AND status = 'ok')`, user).Scan(&kept)
	})
	switch {
	case err != nil:
		s.opts.Log.Warn("the Armature connection could not be read for a sign-in's exchange", "error", err)
		return false, tokenexchange.Why(partnerName, tokenexchange.ErrUnreachable)
	case app == nil, kept:
		return false, ""
	}
	if app.Offered == nil {
		// Never asked since an upgrade: ask now rather than tell the person Armature lacks what it may well have.
		if meta, err := s.describe(ctx, app.BaseURL); err == nil {
			s.noteOffers(ctx, app.BaseURL, meta)
			app.Offered = meta.Capabilities
		}
	}
	if !slices.Contains(app.Offered, tokenexchange.Capability) {
		return false, "The connected Armature does not offer " + tokenexchange.Capability + ", so Exciter cannot connect you there at sign-in. Connect with a token below, or ask an administrator to update Armature."
	}
	if app.ClientID == "" || app.ClientSecret == "" {
		return false, "Exciter has no client id and secret for Armature yet, so you were not connected there at sign-in. An Exciter administrator adds them under Settings, Armature; until then, connect with a token below."
	}

	tokens, err := tokenexchange.Exchange(ctx, s.client.http, app.App, subject)
	if err != nil {
		s.opts.Log.Info("Armature did not exchange a sign-in's token", "user_id", user, "error", err)
		return false, tokenexchange.Why(partnerName, err)
	}
	me, err := s.client.As(app.BaseURL, tokens.Access).Me(ctx)
	switch {
	case err != nil:
		s.opts.Log.Info("Armature did not answer who an exchanged token is", "user_id", user, "error", err)
		return false, tokenexchange.Why(partnerName, tokenexchange.ErrUnreachable)
	case !(&endpoint{ArmatureOrgID: app.ArmatureOrg}).owns(me):
		return false, "Armature answered the sign-in for another organization than the one connected at " + app.BaseURL + ", so you were not connected there. Ask an administrator to check the connection."
	}
	if err := s.storeExchanged(ctx, app, user, tokens, me); err != nil {
		s.opts.Log.Warn("an exchanged Armature token could not be stored", "user_id", user, "error", err)
		return false, tokenexchange.Why(partnerName, tokenexchange.ErrUnreachable)
	}
	s.learnOrg(ctx, &endpoint{BaseURL: app.BaseURL, ArmatureOrgID: app.ArmatureOrg}, me)
	return true, ""
}

// storeExchanged keeps the tokens as the person's, in a new row so the cache keyed by its id starts afresh. A
// token the person stored meanwhile and that works stays, and the new grant is dropped.
func (s *Service) storeExchanged(ctx context.Context, app *partnerApp, user uuid.UUID, tokens *tokenexchange.Tokens, me *Me) error {
	access, err := s.box.Seal([]byte(tokens.Access), tokenContext(app.Installation, user))
	if err != nil {
		return err
	}
	refresh, err := s.box.Seal([]byte(tokens.Refresh), refreshContext(app.Installation, user))
	if err != nil {
		return err
	}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM armature_token WHERE user_id = $1 AND (source = 'exchange' OR status <> 'ok')`, user); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO armature_token (user_id, token, armature_user_id, armature_user_name, armature_user_email, read_only, status,
			                            checked_at, source, refresh_token, access_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'ok', now(), 'exchange', $7, $8)
			ON CONFLICT DO NOTHING`,
			user, access, me.User.ID, me.User.Name, me.User.Email, me.ReadOnly, refresh, tokens.ExpiresAt)
		return err
	})
	return err
}

// renew refreshes the person's grant from a sign-in, one refresh at a time: the row is locked while Armature is
// asked, as a rotated refresh token presented twice ends the grant there. A grant Armature ended is forgotten, and
// the account page says so. patient leaves a refresh that failed within RefreshBackoff as it is.
func (s *Service) renew(ctx context.Context, user, rowID uuid.UUID, patient bool) (*tokenRow, Status, error) {
	var (
		row    *tokenRow
		status = StatusOK
		ended  bool
	)
	_, err := s.db.WriteAdmin(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		var locked uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM armature_token WHERE id = $1 FOR UPDATE`, rowID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			status = StatusNotConnected
			return nil
		}
		if err != nil {
			return err
		}
		if row, err = s.ownToken(db.WithUser(ctx, user), tx); err != nil || row == nil {
			return err
		}
		// Another request renewed it while this one waited for the lock.
		if row.Status == StatusOK && row.ExpiresAt != nil && !tokenexchange.Due(*row.ExpiresAt, time.Now()) {
			return nil
		}
		if patient && row.RefreshFailedAt != nil && time.Since(*row.RefreshFailedAt) < tokenexchange.RefreshBackoff {
			status = backedOff(row)
			return nil
		}
		app, err := s.partnerApp(ctx, tx)
		if err != nil || app == nil {
			status = StatusNotConfigured
			return err
		}
		refresh, err := s.box.Open(row.RefreshSealed, refreshContext(app.Installation, user))
		if err != nil {
			s.opts.Log.Warn("a stored Armature refresh token could not be opened", "token_id", row.ID, "error", err)
			status = StatusRejected
			return nil
		}
		callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
		defer cancel()
		tokens, err := tokenexchange.Refresh(callCtx, s.client.http, app.App, string(refresh))
		var refused *tokenexchange.Refusal
		switch {
		case errors.As(err, &refused) && refused.Ended():
			ended, status = true, StatusNotConnected
			_, err = tx.Exec(ctx, `DELETE FROM armature_token WHERE id = $1`, row.ID)
			return err
		case errors.As(err, &refused):
			s.opts.Log.Warn("Armature refused to refresh a grant from a sign-in", "token_id", row.ID, "error", err)
			status = StatusRejected
			_, err = tx.Exec(ctx, `UPDATE armature_token SET status = 'rejected', checked_at = now(), refresh_failed_at = now() WHERE id = $1`, row.ID)
			return err
		case err != nil:
			s.opts.Log.Info("Armature did not answer a refresh", "token_id", row.ID, "error", err)
			status = StatusUnreachable
			_, err = tx.Exec(ctx, `UPDATE armature_token SET refresh_failed_at = now() WHERE id = $1`, row.ID)
			return err
		}
		access, err := s.box.Seal([]byte(tokens.Access), tokenContext(app.Installation, user))
		if err != nil {
			return err
		}
		rotated, err := s.box.Seal([]byte(tokens.Refresh), refreshContext(app.Installation, user))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE armature_token SET token = $2, refresh_token = $3, access_expires_at = $4, status = 'ok', checked_at = now(),
			    refresh_failed_at = NULL
			WHERE id = $1`, row.ID, access, rotated, tokens.ExpiresAt); err != nil {
			return err
		}
		row.Sealed, row.RefreshSealed, row.ExpiresAt, row.Status, row.RefreshFailedAt = access, rotated, &tokens.ExpiresAt, StatusOK, nil
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if ended {
		why := "Armature ended the connection your sign-in made, so Exciter no longer acts for you there. Sign in again to connect, or connect with a token below."
		if _, err := tokenexchange.Note(ctx, s.db, tokenexchange.PartnerArmature, user, false, why); err != nil {
			s.opts.Log.Warn("an ended Armature grant could not be noted", "user_id", user, "error", err)
		}
	}
	if status != StatusOK {
		return nil, status, nil
	}
	return row, StatusOK, nil
}

// backedOff is what a grant whose refresh failed lately answers while it waits: refused, or not answered.
func backedOff(row *tokenRow) Status {
	if row.Status == StatusRejected {
		return StatusRejected
	}
	return StatusUnreachable
}
