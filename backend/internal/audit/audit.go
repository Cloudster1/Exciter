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
	// ActionStatorConnected and ActionStatorDisconnected keep where the members' Stator tokens went (#40).
	ActionStatorConnected    = "stator.connected"
	ActionStatorDisconnected = "stator.disconnected"
	// ActionWebhookCreated to ActionWebhookDisabled keep the installation's outbound webhooks (#48); the worker turns
	// a failing one off with nobody as the actor.
	ActionWebhookCreated       = "webhook.created"
	ActionWebhookUpdated       = "webhook.updated"
	ActionWebhookDeleted       = "webhook.deleted"
	ActionWebhookSecretRotated = "webhook.secret_rotated"
	ActionWebhookDisabled      = "webhook.disabled"
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
	// ActionCoverageRuleSaved and ActionCoverageRuleRemoved keep what a project's coverage asks of each type (#38).
	ActionCoverageRuleSaved   = "coverage_rule.saved"
	ActionCoverageRuleRemoved = "coverage_rule.removed"
	// ActionTraceModelSaved is a project's trace levels set or cleared (#151).
	ActionTraceModelSaved = "trace_model.saved"
	// ActionSpecificationExported is a specification or a baseline leaving as a document to sign and send (#45).
	ActionSpecificationExported = "specification.exported"
	// ActionProjectSignaturesSet is a project's approvals made electronic signatures, or no longer (#46).
	ActionProjectSignaturesSet = "project.signatures_set"
	// ActionProjectLibrarySet is a project marked a library whose requirements other projects reuse, or no longer (#49).
	ActionProjectLibrarySet = "project.library_set"
	// ActionProjectLanguageSet is a project naming the language its requirements are written in (#207).
	ActionProjectLanguageSet = "project.language_set"
	// ActionQualitySettingsSaved is a project's writing rules and word lists set (#207).
	ActionQualitySettingsSaved = "quality_settings.saved"
	// ActionTermDeleted is a term deleted from a project's glossary, and ActionGlossariesChosen the library glossaries
	// a project uses set (#210).
	ActionTermDeleted      = "term.deleted"
	ActionGlossariesChosen = "glossaries.chosen"
	// ActionDuplicateSettingsSaved is how alike two requirements must be for a project to list them set (#208).
	ActionDuplicateSettingsSaved = "duplicate_settings.saved"
	// ActionOAuthGranted is a person connecting a client through OAuth, and what they let it do (#188).
	ActionOAuthGranted = "oauth.granted"
	// ActionOAuthRevoked is a connection ended, with who ended it: its person, an administrator, the client, or a
	// reused token (#189).
	ActionOAuthRevoked = "oauth.revoked"
	// ActionInstallationOAuthSwitched is OAuth connections switched on or off for the installation (#188).
	ActionInstallationOAuthSwitched = "installation.oauth_switched"
	// ActionBaselineSigned, ActionBaselineSigningSaved and ActionBaselineSigningRemoved keep who released a baseline and
	// who may (#205).
	ActionBaselineSigned         = "baseline.signed"
	ActionBaselineSigningSaved   = "baseline_signing.saved"
	ActionBaselineSigningRemoved = "baseline_signing.removed"
	// ActionRequirementLocked, ActionRequirementUnlocked and ActionStatusLocksSet keep what holds requirements as they are
	// (#205).
	ActionRequirementLocked   = "requirement.locked"
	ActionRequirementUnlocked = "requirement.unlocked"
	ActionStatusLocksSet      = "status.locks_set"
	// ActionFieldRuleSaved keeps when a field is required and who changes it (#206).
	ActionFieldRuleSaved = "field_rule.saved"
	// ActionReviewSessionOpened, ActionReviewSessionRoundStarted and ActionReviewSessionClosed keep how a review session
	// went (#204).
	ActionReviewSessionOpened       = "review_session.opened"
	ActionReviewSessionRoundStarted = "review_session.round_started"
	ActionReviewSessionClosed       = "review_session.closed"
	// ActionEvidenceSettingsSaved keeps which branch a project's test results count on (#200).
	ActionEvidenceSettingsSaved = "evidence_settings.saved"
	// ActionCodeTagWordsSaved keeps the words a project's code tags use (#201).
	ActionCodeTagWordsSaved = "code_tag_words.saved"
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
	ActionBaselineDeleted, ActionCoverageRuleSaved, ActionCoverageRuleRemoved, ActionStatorConnected, ActionStatorDisconnected,
	ActionSpecificationExported, ActionProjectSignaturesSet, ActionWebhookCreated, ActionWebhookUpdated, ActionWebhookDeleted,
	ActionWebhookSecretRotated, ActionWebhookDisabled, ActionProjectLibrarySet, ActionTraceModelSaved, ActionOAuthGranted,
	ActionInstallationOAuthSwitched, ActionOAuthRevoked, ActionBaselineSigned, ActionBaselineSigningSaved, ActionBaselineSigningRemoved,
	ActionRequirementLocked, ActionRequirementUnlocked, ActionStatusLocksSet, ActionFieldRuleSaved,
	ActionReviewSessionOpened, ActionReviewSessionRoundStarted, ActionReviewSessionClosed, ActionEvidenceSettingsSaved,
	ActionCodeTagWordsSaved, ActionProjectLanguageSet, ActionQualitySettingsSaved,
	ActionTermDeleted, ActionGlossariesChosen, ActionDuplicateSettingsSaved,
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
