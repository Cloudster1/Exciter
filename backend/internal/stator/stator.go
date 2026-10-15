// Package stator connects Exciter to Stator, the wiki (M4); docs/api-contract-m4.md says how, and the shapes here
// are the agreed ones. It follows internal/armature wherever the two integrations ask the same.
package stator

import (
	"time"

	"github.com/google/uuid"
)

// Agreed limits and names, Armature's integration's where it has them.
const (
	// TokenPrefix starts every Stator personal access token; WebhookSecretPrefix every webhook secret.
	TokenPrefix         = "stator_pat_"
	WebhookSecretPrefix = "stator_whs_"
	// Product is what Stator answers as product on GET /meta, which a new address must.
	Product = "Stator"
	// RemoteLinksCapability is what Stator lists among its capabilities on GET /meta once it takes remote links
	// on pages (#42); until then nothing is put on a page.
	RemoteLinksCapability = "page.remote-links"
	// SignatureHeader carries sha256= and the hex HMAC of the raw body; EventHeader the topic.
	SignatureHeader = "X-Stator-Signature-256"
	EventHeader     = "X-Stator-Event"
	// LinkSource is what Stator shows as the source of the remote links Exciter puts on pages.
	LinkSource = "Exciter"
	// MaxLookupPages bounds one batch of pages to look up.
	MaxLookupPages = 50
	// LookupParallel bounds the fetches one lookup makes at once.
	LookupParallel = 8
	// SearchDefaultLimit and SearchMaxLimit bound the picker's answer, Stator's quick search's; MaxSearchLength
	// what it sends Stator.
	SearchDefaultLimit = 8
	SearchMaxLimit     = 20
	MaxSearchLength    = 200
	// MaxLinkTitleLength bounds a remote link's title, as Armature's does.
	MaxLinkTitleLength = 255

	// PageCacheTTL and SearchCacheTTL are how long an answer is served to the same person without asking Stator
	// again; webhooks end a page's sooner.
	PageCacheTTL   = 60 * time.Second
	SearchCacheTTL = 60 * time.Second
	// CallTimeout bounds one call to Stator from a request.
	CallTimeout = 5 * time.Second
	// MaxResponseBytes bounds one Stator answer, a page's body included; WebhookMaxBytes a delivery's body.
	MaxResponseBytes = 4 << 20
	WebhookMaxBytes  = 1 << 20
	// WebhookReplayWindow is how long a delivered event id is remembered, so a replay changes nothing.
	WebhookReplayWindow = 24 * time.Hour
)

// Topics Stator sends that the receiver acts on (#41), and the one Exciter asks Stator for (#42).
const (
	TopicPagePublished     = "page.published"
	TopicPageMoved         = "page.moved"
	TopicPageDeleted       = "page.deleted"
	TopicRemoteLinkRemoved = "page.remote_link.removed"
)

// WebhookTopics are the topics the admin screen asks for; Stator offers the last only once it ships #42's API.
var WebhookTopics = []string{TopicPagePublished, TopicPageMoved, TopicPageDeleted, TopicRemoteLinkRemoved}

// Status says why a read could not ask Stator, or ok when it did.
type Status string

const (
	StatusOK            Status = "ok"
	StatusNotConfigured Status = "not_configured"
	StatusNotConnected  Status = "not_connected"
	StatusRejected      Status = "rejected"
	StatusUnreachable   Status = "unreachable"
)

// Statuses lists every Status, for the API document.
var Statuses = []Status{StatusOK, StatusNotConfigured, StatusNotConnected, StatusRejected, StatusUnreachable}

// Connection is the installation's Stator: its address is where people open it and which Stator it is.
type Connection struct {
	BaseURL string `json:"baseUrl"`
	// RemoteLinks says Stator listed RemoteLinksCapability when the address was saved or last checked.
	RemoteLinks bool `json:"remoteLinks"`
	// WebhookSecretSet says whether a secret is stored; the secret itself is never answered.
	WebhookSecretSet bool `json:"webhookSecretSet"`
	// WebhookURL is where Stator sends webhooks, and WebhookTopics what to subscribe it to.
	WebhookURL    string    `json:"webhookUrl"`
	WebhookTopics []string  `json:"webhookTopics"`
	Connected     int       `json:"connected"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// ConnectionInput sets the connection; WebhookSecret absent keeps the stored one and empty removes it.
type ConnectionInput struct {
	BaseURL       string  `json:"baseUrl"`
	WebhookSecret *string `json:"webhookSecret,omitempty"`
}

// Account is the caller's own standing with Stator.
type Account struct {
	Configured bool   `json:"configured"`
	BaseURL    string `json:"baseUrl"`
	Connected  bool   `json:"connected"`
	Status     Status `json:"status"`
	// User is whom the token acts as in Stator.
	User      *AccountUser `json:"user"`
	CheckedAt *time.Time   `json:"checkedAt"`
}

// AccountUser is a person in Stator.
type AccountUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// TokenInput stores a person's Stator token.
type TokenInput struct {
	Token string `json:"token"`
}

// Page is a Stator page as the viewer may read it: its published version, never its body.
type Page struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	// Path is the titles of the pages above it, the space's home first.
	Path          []string  `json:"path"`
	Version       int       `json:"version"`
	URL           string    `json:"url"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedByName string    `json:"updatedByName"`
}

// LookupResult is one page asked for, null when the viewer may not read it or it is gone.
type LookupResult struct {
	ID   uuid.UUID `json:"id"`
	Page *Page     `json:"page"`
}

// Relation says what a page is to a requirement, read from the requirement: it is specified by, described by or
// related to the page.
type Relation string

const (
	Specifies Relation = "specifies"
	Describes Relation = "describes"
	Relates   Relation = "relates"
)

// Relations lists every Relation, in the order the picker offers them.
var Relations = []Relation{Specifies, Describes, Relates}

// SyncState is how far the remote link on the page has come (#42); none while Stator takes no remote links.
type SyncState string

const (
	SyncSynced  SyncState = "synced"
	SyncPending SyncState = "pending"
	SyncFailed  SyncState = "failed"
	SyncNone    SyncState = "none"
)

// SyncStates lists every SyncState, for the API document.
var SyncStates = []SyncState{SyncSynced, SyncPending, SyncFailed, SyncNone}

// Link traces a requirement to a page. The page's id, space and latest published version are stored for
// everyone; its title and the rest are read live as the viewer.
type Link struct {
	ID       uuid.UUID `json:"id"`
	Relation Relation  `json:"relation"`
	PageID   uuid.UUID `json:"pageId"`
	SpaceKey string    `json:"spaceKey"`
	URL      string    `json:"url"`
	// PageVersion is the page's latest published version Exciter knows of, and PageVersionSeen the one the link
	// was made or last confirmed against; Suspect says the first is newer.
	PageVersion     int  `json:"pageVersion"`
	PageVersionSeen int  `json:"pageVersionSeen"`
	Suspect         bool `json:"suspect"`
	// ReviewedRevision is the requirement's revision the link was made or last confirmed against.
	ReviewedRevision int        `json:"reviewedRevision"`
	Sync             SyncState  `json:"sync"`
	Error            string     `json:"error"`
	BrokenAt         *time.Time `json:"brokenAt"`
	DetachedAt       *time.Time `json:"detachedAt"`
	CreatedByName    string     `json:"createdByName"`
	CreatedAt        time.Time  `json:"createdAt"`
}

// LinkInput traces a requirement to a page by its id.
type LinkInput struct {
	PageID   uuid.UUID `json:"pageId"`
	Relation Relation  `json:"relation"`
}

// ClearInput confirms a link against the page's version the caller read, with why.
type ClearInput struct {
	PageVersion int    `json:"pageVersion"`
	Comment     string `json:"comment"`
}
