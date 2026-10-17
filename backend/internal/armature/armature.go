// Package armature connects Exciter to Armature, the tracker (M2); docs/api-contract-m2.md says how, and the
// shapes here are the agreed ones.
package armature

import (
	"time"

	"github.com/cloudster1/exciter/backend/internal/via"
	"github.com/google/uuid"
)

// Agreed limits and names, Stator's where Stator has them.
const (
	// TokenPrefix starts every Armature personal access token; WebhookSecretPrefix every webhook secret.
	TokenPrefix         = "armature_pat_"
	WebhookSecretPrefix = "armature_whs_"
	// ScopeRead is the scope of an Armature token that may only read.
	ScopeRead = "read"
	// SignatureHeader carries sha256= and the hex HMAC of the raw body; the timestamped pair signs
	// "{timestamp}.{body}" (#29).
	SignatureHeader            = "X-Armature-Signature-256"
	TimestampHeader            = "X-Armature-Timestamp"
	TimestampedSignatureHeader = "X-Armature-Signature-Timestamped-256"
	// VersionHeader names the envelope's version, so a delivery Exciter cannot read is refused before parsing.
	VersionHeader = "X-Armature-Webhook-Version"
	// LinkSource is what Armature shows as the source of the remote links Exciter puts on issues.
	LinkSource = "Exciter"
	// KeyPattern is an issue key as Exciter stores it, Armature's shape in upper case.
	KeyPattern = `^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$`
	// MaxLookupKeys bounds one batch of keys to look up; MaxCreateItems one batch of issues to make.
	MaxLookupKeys  = 50
	MaxCreateItems = 50
	// LookupParallel bounds the fetches one lookup makes at once.
	LookupParallel = 8
	// SearchDefaultLimit and SearchMaxLimit bound the picker's answer; MaxSearchLength what it sends Armature.
	SearchDefaultLimit = 10
	SearchMaxLimit     = 20
	MaxSearchLength    = 200
	// MaxSummaryLength and MaxLinkTitleLength are Armature's limits on an issue's summary and a link's title.
	MaxSummaryLength   = 255
	MaxLinkTitleLength = 255

	// IssueCacheTTL and SearchCacheTTL are how long an answer is served to the same person without asking
	// Armature again; webhooks end it sooner.
	IssueCacheTTL  = 60 * time.Second
	SearchCacheTTL = 60 * time.Second
	// MetaCacheTTL covers projects and issue types, which no webhook announces; ThemeCacheTTL a followed theme.
	MetaCacheTTL  = 5 * time.Minute
	ThemeCacheTTL = 5 * time.Minute
	// CallTimeout bounds one call to Armature from a request; ThemeTimeout is shorter, read on every load.
	CallTimeout  = 5 * time.Second
	ThemeTimeout = 2 * time.Second
	// MaxResponseBytes bounds one Armature answer; WebhookMaxBytes a delivery's body.
	MaxResponseBytes = 4 << 20
	WebhookMaxBytes  = 1 << 20
	// WebhookReplayWindow is how long a delivered event id is remembered, so a replay changes nothing.
	WebhookReplayWindow = 24 * time.Hour
)

// StatusCategories are Armature's, in workflow order.
var StatusCategories = []string{"todo", "in_progress", "done"}

// WebhookTopics are the Armature topics the receiver acts on (#29); the admin screen names them.
var WebhookTopics = []string{TopicIssueCreated, TopicIssueUpdated, TopicIssueTransitioned, TopicIssueDeleted, TopicRemoteLinkRemoved}

// Status says why a read could not ask Armature, or ok when it did.
type Status string

const (
	StatusOK            Status = "ok"
	StatusNotConfigured Status = "not_configured"
	StatusNotConnected  Status = "not_connected"
	StatusRejected      Status = "rejected"
	StatusUnreachable   Status = "unreachable"
	// StatusOutdated is an Armature that does not offer what the read needs; the account's missing says what.
	StatusOutdated Status = "outdated"
)

// Statuses lists every Status, for the API document.
var Statuses = []Status{StatusOK, StatusNotConfigured, StatusNotConnected, StatusRejected, StatusUnreachable, StatusOutdated}

// Connection is the installation's Armature: its address is where people open it and which Armature it is.
type Connection struct {
	BaseURL string `json:"baseUrl"`
	// ArmatureOrgID is learned from the first token that checks out; tokens and webhooks from any other are refused.
	ArmatureOrgID *uuid.UUID `json:"armatureOrgId"`
	// WebhookSecretSet says whether a secret is stored; the secret itself is never answered.
	WebhookSecretSet bool `json:"webhookSecretSet"`
	// WebhookURL is where Armature sends webhooks, and WebhookTopics what to subscribe it to.
	WebhookURL    string    `json:"webhookUrl"`
	WebhookTopics []string  `json:"webhookTopics"`
	Connected     int       `json:"connected"`
	UpdatedAt     time.Time `json:"updatedAt"`
	// Capabilities are what Armature listed on GET /meta when last asked, null before; Missing are those Exciter
	// needs and did not find, each with the sentence that says what goes without it.
	Capabilities []string  `json:"capabilities"`
	Missing      []Missing `json:"missing"`
}

// ConnectionInput sets the connection; WebhookSecret absent keeps the stored one and empty removes it.
type ConnectionInput struct {
	BaseURL       string  `json:"baseUrl"`
	WebhookSecret *string `json:"webhookSecret,omitempty"`
}

// Account is the caller's own standing with Armature.
type Account struct {
	Configured bool   `json:"configured"`
	BaseURL    string `json:"baseUrl"`
	Connected  bool   `json:"connected"`
	Status     Status `json:"status"`
	// User is whom the token acts as in Armature; ReadOnly warns that it cannot make issues or links.
	User      *AccountUser `json:"user"`
	ReadOnly  bool         `json:"readOnly"`
	CheckedAt *time.Time   `json:"checkedAt"`
	// Missing are the capabilities Exciter needs that the connected Armature does not offer.
	Missing []Missing `json:"missing"`
}

// AccountUser is a person in Armature.
type AccountUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// TokenInput stores a person's Armature token.
type TokenInput struct {
	Token string `json:"token"`
}

// IssueStatus is an issue's status and its category, todo, in_progress or done.
type IssueStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// Issue is an Armature issue as the viewer may see it.
type Issue struct {
	Key          string      `json:"key"`
	Summary      string      `json:"summary"`
	URL          string      `json:"url"`
	Type         string      `json:"type"`
	Status       IssueStatus `json:"status"`
	AssigneeName string      `json:"assigneeName"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

// LookupResult is one key asked and the issue it names, null when the viewer may not see one.
type LookupResult struct {
	Key   string `json:"key"`
	Issue *Issue `json:"issue"`
}

// Relation says what a requirement is to an issue.
type Relation string

const (
	Implements Relation = "implements"
	Verifies   Relation = "verifies"
	Relates    Relation = "relates"
)

// Relations lists every Relation, in the order the picker offers them.
var Relations = []Relation{Implements, Verifies, Relates}

// SyncState is how far the remote link on the issue has come.
type SyncState string

const (
	SyncSynced  SyncState = "synced"
	SyncPending SyncState = "pending"
	SyncFailed  SyncState = "failed"
)

// SyncStates lists every SyncState, for the API document.
var SyncStates = []SyncState{SyncSynced, SyncPending, SyncFailed}

// Link traces a requirement to an issue. StatusCategory is stored for everyone; the rest of the issue is read
// live as the viewer.
type Link struct {
	ID             uuid.UUID  `json:"id"`
	Relation       Relation   `json:"relation"`
	IssueKey       string     `json:"issueKey"`
	StatusCategory string     `json:"statusCategory"`
	StatusStale    bool       `json:"statusStale"`
	Sync           SyncState  `json:"sync"`
	Error          string     `json:"error"`
	BrokenAt       *time.Time `json:"brokenAt"`
	DetachedAt     *time.Time `json:"detachedAt"`
	// ReviewedRevision is the requirement's revision the link was made or last confirmed against (#36), and Suspect
	// says the requirement has a newer one.
	ReviewedRevision int       `json:"reviewedRevision"`
	Suspect          bool      `json:"suspect"`
	CreatedByName    string    `json:"createdByName"`
	CreatedAt        time.Time `json:"createdAt"`
	// Via is the token or connected app it was made through, null for a session (#190).
	Via *via.Via `json:"via"`
}

// LinkInput traces a requirement to an issue by its key.
type LinkInput struct {
	IssueKey string   `json:"issueKey"`
	Relation Relation `json:"relation"`
}

// Project is an Armature project the viewer may see, and whether they may file issues in it.
type Project struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	CanCreate bool   `json:"canCreate"`
}

// IssueType is a kind of issue Armature files.
type IssueType struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Icon string    `json:"icon"`
}

// CreateInput makes one issue per requirement in an Armature project, each linked to its requirement.
type CreateInput struct {
	RequirementKeys []string   `json:"requirementKeys"`
	ArmatureProject string     `json:"armatureProject"`
	TypeID          *uuid.UUID `json:"typeId,omitempty"`
	Relation        Relation   `json:"relation"`
}

// Created is one issue made and the link that traces its requirement to it.
type Created struct {
	RequirementKey string `json:"requirementKey"`
	Issue          Issue  `json:"issue"`
	Link           Link   `json:"link"`
}

// CreateFailure is the first requirement Armature refused, and why.
type CreateFailure struct {
	RequirementKey string `json:"requirementKey"`
	Code           string `json:"code"`
	Message        string `json:"message"`
}

// Follow is whether the caller follows their Armature theme, and why it is not shown when it is not.
type Follow struct {
	Following bool   `json:"following"`
	Status    Status `json:"status"`
	Error     string `json:"error"`
}

// ClearInput confirms an Armature link against the requirement's revision the caller read (#36).
type ClearInput struct {
	Revision int    `json:"revision"`
	Comment  string `json:"comment"`
}
