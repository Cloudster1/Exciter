// Package audit is the installation's record of who did what to it, written in
// the same transaction as the act so the two cannot disagree.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Actions recorded so far; each feature adds its own as it lands.
const (
	// ActionEventDeadLettered is an outbox event the worker gave up on after
	// MaxAttempts, recorded with nobody as its actor so an administrator sees it.
	ActionEventDeadLettered = "event.dead_lettered"
	// ActionAuditExported is the record itself leaving as a file.
	ActionAuditExported = "audit.exported"
	// ActionMemberCreated is a person an administrator added with a password.
	ActionMemberCreated = "member.created"
	// ActionMemberRoleChanged is a member's standing an administrator, or the identity provider's groups, changed.
	ActionMemberRoleChanged = "member.role_changed"
	// ActionMemberRemoved is somebody taken out before letting go meant switching off (#86); older entries keep it.
	ActionMemberRemoved = "member.removed"
	// ActionMemberSwitchedOff and ActionMemberSwitchedOn are an administrator letting somebody go and back.
	ActionMemberSwitchedOff = "member.switched_off"
	ActionMemberSwitchedOn  = "member.switched_on"
	// ActionMemberJoined is somebody let in by a mapped group on their first sign-in, with nobody as its actor.
	ActionMemberJoined = "member.joined"
	// ActionMemberAdmitted and ActionMemberDeclined answer somebody waiting to be let in.
	ActionMemberAdmitted   = "member.admitted"
	ActionMemberDeclined   = "member.declined"
	ActionSSOProviderSaved = "sso.provider_saved"
	ActionGroupRoleSet     = "sso.group_role_set"
	ActionGroupRoleRemoved = "sso.group_role_removed"
	// ActionThemeDefaultSet is the theme the installation shows to whoever has not chosen one, named or cleared.
	ActionThemeDefaultSet = "theme.default_set"
	// Projects (#11): made, given a new key, archived and back, and deleted for good.
	ActionProjectCreated    = "project.created"
	ActionProjectKeyChanged = "project.key_changed"
	ActionProjectArchived   = "project.archived"
	ActionProjectUnarchived = "project.unarchived"
	ActionProjectDeleted    = "project.deleted"
	// ActionProjectGrantsChanged keeps a project's grants before and after.
	ActionProjectGrantsChanged = "project.grants_changed"
	// ActionRequirementPurged and ActionTrashEmptied keep which keys went for good, since their history goes too.
	ActionRequirementPurged = "requirement.purged"
	ActionTrashEmptied      = "trash.emptied"
	// ActionWorkflowSaved and ActionWorkflowAssigned keep a workflow's shape and who follows it.
	ActionWorkflowSaved    = "workflow.saved"
	ActionWorkflowAssigned = "workflow.assigned"
	// ActionTokenCreated and ActionTokenRevoked keep who could act through a personal access token, and when.
	ActionTokenCreated = "token.created"
	ActionTokenRevoked = "token.revoked"
	// ActionArmatureConnected and ActionArmatureDisconnected keep where the members' Armature tokens went.
	ActionArmatureConnected    = "armature.connected"
	ActionArmatureDisconnected = "armature.disconnected"
	// ActionCommentDeleted keeps an administrator deleting somebody else's comment (#33).
	ActionCommentDeleted = "comment.deleted"
	// ActionLinkTypeCreated, ActionLinkTypeChanged and ActionLinkTypeDeleted keep the installation's and the
	// projects' link types, and how a project uses one.
	ActionLinkTypeCreated = "link_type.created"
	ActionLinkTypeChanged = "link_type.changed"
	ActionLinkTypeDeleted = "link_type.deleted"
	// ActionInstallationRenamed keeps what the installation was called before.
	ActionInstallationRenamed = "installation.renamed"
	// ActionApprovalRuleSaved and ActionApprovalRuleRemoved keep who approves a project's requirements, and
	// ActionReviewWithdrawn an administrator withdrawing somebody else's review (#35).
	ActionApprovalRuleSaved   = "approval_rule.saved"
	ActionApprovalRuleRemoved = "approval_rule.removed"
	ActionReviewWithdrawn     = "review.withdrawn"
	// ActionBaselineTaken, ActionBaselineLocked and ActionBaselineDeleted keep a project's baselines (#37).
	ActionBaselineTaken   = "baseline.taken"
	ActionBaselineLocked  = "baseline.locked"
	ActionBaselineDeleted = "baseline.deleted"
)

// Actions is every action the log may hold, for a filter to offer and a
// client to name.
var Actions = []string{
	ActionEventDeadLettered, ActionAuditExported, ActionMemberCreated, ActionMemberRoleChanged, ActionMemberRemoved,
	ActionMemberSwitchedOff, ActionMemberSwitchedOn, ActionMemberJoined, ActionMemberAdmitted, ActionMemberDeclined, ActionSSOProviderSaved, ActionGroupRoleSet, ActionGroupRoleRemoved,
	ActionThemeDefaultSet, ActionProjectCreated, ActionProjectKeyChanged, ActionProjectArchived, ActionProjectUnarchived,
	ActionProjectDeleted, ActionProjectGrantsChanged, ActionRequirementPurged, ActionTrashEmptied,
	ActionWorkflowSaved, ActionWorkflowAssigned, ActionTokenCreated, ActionTokenRevoked, ActionLinkTypeCreated, ActionLinkTypeChanged,
	ActionLinkTypeDeleted, ActionArmatureConnected, ActionArmatureDisconnected, ActionInstallationRenamed, ActionCommentDeleted,
	ActionApprovalRuleSaved, ActionApprovalRuleRemoved, ActionReviewWithdrawn, ActionBaselineTaken, ActionBaselineLocked,
	ActionBaselineDeleted,
}

// Redacted stands in the record for a value that looked like a credential.
const Redacted = "[redacted]"

// SecretPrefixes start every credential Exciter, Armature or Stator issues, so a
// value that starts with one never reaches the record whatever key it hides under.
var SecretPrefixes = []string{"exciter_pat_", "exciter_whs_", "armature_pat_", "armature_whs_", "stator_pat_", "stator_whs_"}

// secretWords mark a key whose value would be a credential if it were text.
var secretWords = []string{"secret", "password", "token", "credential"}

// secretStates are the words a secret's key may carry instead of the secret.
var secretStates = map[string]bool{"set": true, "kept": true, "removed": true}

// Entry is one act to record. A blank IP takes the one WithIP put on the context.
type Entry struct {
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Actor      uuid.UUID
	Data       map[string]any
	IP         string
}

type ipKey struct{}

// WithIP carries the caller's address to every entry written for them.
func WithIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

func ipFrom(ctx context.Context) string {
	ip, _ := ctx.Value(ipKey{}).(string)
	return ip
}

// Write records an entry inside the caller's transaction.
func Write(ctx context.Context, tx db.DBTX, e Entry) error {
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("an audit entry needs an action")
	}
	data := []byte("{}")
	if e.Data != nil {
		var err error
		if data, err = Scrub(e.Data); err != nil {
			return fmt.Errorf("%s: %w", e.Action, err)
		}
	}
	var actor *uuid.UUID
	if e.Actor != uuid.Nil {
		actor = &e.Actor
	}
	ip := e.IP
	if ip == "" {
		ip = ipFrom(ctx)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, action, target_type, target_id, data, ip)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet)`,
		actor, e.Action, e.TargetType, e.TargetID, data, ip)
	return err
}

// Scrub writes an entry's data as JSON with every value that looks like a
// credential replaced by Redacted, so an act never fails over its record.
func Scrub(data map[string]any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(scrub("", v))
}

func scrub(key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			v[k] = scrub(k, inner)
		}
		return v
	case []any:
		for i, inner := range v {
			v[i] = scrub(key, inner)
		}
		return v
	case string:
		for _, prefix := range SecretPrefixes {
			if strings.HasPrefix(strings.TrimSpace(v), prefix) {
				return Redacted
			}
		}
		if secretKey(key) && v != "" && !secretStates[v] {
			return Redacted
		}
	}
	return v
}

func secretKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range secretWords {
		if strings.Contains(k, word) {
			return true
		}
	}
	return false
}
