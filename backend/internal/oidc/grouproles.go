package oidc

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// admittingGroups are the groups claimed that let somebody new in: those a group here carries as its provider
// claim and that hold a grant, by name and once each.
func admittingGroups(granted map[string]bool, claimed []string) []string {
	from := []string{}
	for _, group := range claimed {
		if granted[group] && !slices.Contains(from, group) {
			from = append(from, group)
		}
	}
	slices.Sort(from)
	return from
}

// nextStanding is what a sign-in makes of a membership, given the claimed groups that hold a grant and whether
// such a group lets somebody new in; nil means the person is not let in. Standing is only whether somebody is in:
// what they may do follows their groups' grants, which the sign-in replaces with the provider's.
func nextStanding(current *auth.OrgRole, admitting []string, admit bool) *auth.OrgRole {
	switch {
	case current != nil:
		return current
	case len(admitting) == 0 || !admit:
		// Nobody here said yes, and no group with a grant says it in advance.
		return nil
	}
	member := auth.RoleMember
	return &member
}

// Where somebody was let in by their group, as the audit log names it.
const (
	viaSignIn        = "sign_in"
	viaTokenExchange = "token_exchange"
)

// grantedRefs are the provider claims of the groups that hold a grant.
func grantedRefs(ctx context.Context, tx db.DBTX) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT g.external_ref FROM groups g
		WHERE g.source = 'oidc' AND g.external_ref IS NOT NULL
		  AND EXISTS (SELECT 1 FROM role_assignment a WHERE a.group_id = g.id)`)
	if err != nil {
		return nil, fmt.Errorf("read the groups that hold a grant: %w", err)
	}
	refs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(refs))
	for _, ref := range refs {
		out[ref] = true
	}
	return out, nil
}

// followMapping settles a sign-in's membership inside the sign-in's transaction: somebody new is let in when admit
// is on and one of their groups holds a grant, and the log records which; via says where.
func followMapping(ctx context.Context, tx db.DBTX, userID uuid.UUID, claimed []string, admit bool, via string) (auth.OrgRole, error) {
	var current *auth.OrgRole
	var found auth.OrgRole
	err := tx.QueryRow(ctx, `SELECT role FROM app_user WHERE id = $1 AND role IS NOT NULL FOR UPDATE`, userID).Scan(&found)
	switch {
	case err == nil:
		current = &found
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}
	if current != nil {
		return *current, nil
	}

	granted, err := grantedRefs(ctx, tx)
	if err != nil {
		return "", err
	}
	groups := admittingGroups(granted, claimed)
	next := nextStanding(current, groups, admit)
	if next == nil {
		return "", ErrNotAMember
	}
	if _, err := tx.Exec(ctx, `UPDATE app_user SET role = $2 WHERE id = $1`, userID, string(*next)); err != nil {
		return "", fmt.Errorf("let the person in by their group: %w", err)
	}
	// A request noted before the group held a grant has been answered now.
	if _, err := tx.Exec(ctx, `DELETE FROM join_request WHERE user_id = $1`, userID); err != nil {
		return "", err
	}
	return *next, audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberJoined, TargetType: "user", TargetID: &userID,
		Data: map[string]any{"role": *next, "group": groups[0], "groups": groups, "via": via}})
}
