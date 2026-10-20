// Package stator connects Exciter to Stator, the wiki (M4); docs/api-contract-m4.md says how, and the shapes here
// are the agreed ones. It follows internal/armature wherever the two integrations ask the same.
package stator

import (
	"time"

	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
	"github.com/cloudster1/exciter/backend/internal/via"
	"github.com/google/uuid"
)

// Agreed limits and names, Armature's integration's where it has them.
const (
	// TokenPrefix starts every Stator personal access token; WebhookSecretPrefix every webhook secret.
	TokenPrefix         = "stator_pat_"
	WebhookSecretPrefix = "stator_whs_"
	// Product is what Stator answers as product on GET /meta, which a new address must, in any case.
	Product = "stator"
	// RemoteLinksCapability is what Stator lists on GET /meta once it takes remote links on pages (#42), since
	// its #437; until then nothing is put on a page. Matched exactly, as a raised version promises otherwise.
	RemoteLinksCapability = "page-remote-links@1"
	// SignatureHeader carries sha256= and the hex HMAC of the raw body; EventHeader the topic.
	SignatureHeader = "X-Stator-Signature-256"
	EventHeader     = "X-Stator-Event"
	// VersionHeader is where Stator names the envelope's version once its #412 sends one, Armature's header.
	VersionHeader = "X-Stator-Webhook-Version"
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
	// MaxClientIDLength bounds the client id of Exciter's partner app at Stator.
	MaxClientIDLength = 255

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

// WebhookTopics are the topics the admin screen asks Stator's webhook for.
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
	// ClientID is Exciter's partner app at Stator, which connects people at sign-in (#274); ClientSecretSet says
	// whether its secret is stored, which is never answered.
	ClientID        string `json:"clientId"`
	ClientSecretSet bool   `json:"clientSecretSet"`
	// TokenExchange says the operator lets Exciter connect people at sign-in (EXCITER_STATOR_TOKEN_EXCHANGE), and
	// ExchangeOffered that Stator listed oauth.token-exchange@1 when last asked.
	TokenExchange   bool `json:"tokenExchange"`
	ExchangeOffered bool `json:"exchangeOffered"`
}

// ConnectionInput sets the connection; WebhookSecret and ClientSecret absent keep the stored one and empty removes
// it, ClientID absent keeps the stored one.
type ConnectionInput struct {
	BaseURL       string  `json:"baseUrl"`
	WebhookSecret *string `json:"webhookSecret,omitempty"`
	ClientID      *string `json:"clientId,omitempty"`
	ClientSecret  *string `json:"clientSecret,omitempty"`
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
	// Source is how the person is connected: token for one they stored, sign_in for one their sign-in exchanged,
	// none while they are not.
	Source Source `json:"source"`
	// SignIn is what the last sign-in's exchange came to, null when none tried.
	SignIn *tokenexchange.SignIn `json:"signIn"`
}

// Source is how a person's Stator token came to be stored.
type Source string

const (
	SourceNone   Source = "none"
	SourceToken  Source = "token"
	SourceSignIn Source = "sign_in"
)

// Sources lists every Source, for the API document.
var Sources = []Source{SourceNone, SourceToken, SourceSignIn}

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

// PageHit is a page the picker found, as Stator's quick search answers it: no version, which the link reads.
type PageHit struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	Path      []string  `json:"path"`
	URL       string    `json:"url"`
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
	ReviewedRevision int `json:"reviewedRevision"`
	// Sync is how far Exciter's remote link on the page has come, and Error why it failed, empty otherwise.
	Sync          SyncState  `json:"sync"`
	Error         string     `json:"error"`
	BrokenAt      *time.Time `json:"brokenAt"`
	DetachedAt    *time.Time `json:"detachedAt"`
	CreatedByName string     `json:"createdByName"`
	CreatedAt     time.Time  `json:"createdAt"`
	// Via is the token or connected app it was made through, null for a session (#190).
	Via *via.Via `json:"via"`
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
