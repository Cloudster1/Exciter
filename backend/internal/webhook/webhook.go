// Package webhook posts the installation's requirement events to addresses its administrators named (#48), signed
// with a secret each endpoint was shown once, and holds the envelope version its receivers read (#103). Adapted from Stator's internal/webhook.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
)

// Version is the envelope version a new endpoint is registered for. It stays 1 for at least one Dynamo release after
// VersionCloudEvents, as docs/integration.md says.
const Version = 1

// VersionCloudEvents is the envelope in the CloudEvents 1.0 shape (#133), chosen per endpoint.
const VersionCloudEvents = 2

// Versions are the envelope versions Exciter sends and reads, oldest first.
var Versions = []int{Version, VersionCloudEvents}

// ReadsHeader says whether Exciter reads the version a header names, which a receiver checks before parsing the body;
// an absent header is a sender from before versions.
func ReadsHeader(h string) bool {
	return h == "" || slices.ContainsFunc(Versions, func(v int) bool { return h == strconv.Itoa(v) })
}

// VersionList names the versions Exciter sends and reads, for a sentence.
func VersionList() string {
	names := make([]string, len(Versions))
	for i, v := range Versions {
		names[i] = strconv.Itoa(v)
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

const (
	// MaxAttempts is how many times one event is tried before it is given up.
	MaxAttempts = 6
	// TopicAny subscribes an endpoint to every topic.
	TopicAny = "*"
	// TopicPing is what a test delivery carries.
	TopicPing = "ping"
	// SecretPrefix marks a webhook secret, so one is never taken for a token and never reaches the audit log.
	SecretPrefix = "exciter_whs_"
	// DefaultDeliveries and MaxDeliveries bound one read of the log.
	DefaultDeliveries = 50
	MaxDeliveries     = 200
	// MaxName, MaxURL and MaxProjects are the database's bounds on an endpoint.
	MaxName     = 100
	MaxURL      = 2000
	MaxProjects = 100
	// DisableAfter is how long an endpoint fails every attempt before the worker turns it off, and
	// DisableAfterFailures the fewest failures that do, so a receiver down for an afternoon is kept.
	DisableAfter         = 24 * time.Hour
	DisableAfterFailures = MaxAttempts
	// The headers a delivery carries, named as Armature and Stator name theirs. The timestamped signature also
	// covers when the attempt was sent, so a receiver can refuse one replayed later.
	SignatureHeader            = "X-Exciter-Signature-256"
	TimestampHeader            = "X-Exciter-Timestamp"
	TimestampedSignatureHeader = "X-Exciter-Signature-Timestamped-256"
	EventHeader                = "X-Exciter-Event"
	DeliveryHeader             = "X-Exciter-Delivery"
	VersionHeader              = "X-Exciter-Webhook-Version"
	userAgent                  = "Exciter-Webhook"
	// Source is what every version 1 envelope names as its sender.
	Source = "exciter"
	// TypePrefix leads a version 2 envelope's type, the topic after it, as Armature's and Stator's lead theirs.
	TypePrefix = "dev.exciter."
	// SpecVersion is the CloudEvents version a version 2 envelope follows.
	SpecVersion = "1.0"
	// DataVersion is the version of the payload a version 2 envelope carries as its data, version 1's payload.
	DataVersion = 1
	// Content types of the two envelopes; version 2 is CloudEvents' structured mode.
	ContentTypeJSON        = "application/json"
	ContentTypeCloudEvents = "application/cloudevents+json; charset=utf-8"
)

// Backoff is the wait before each retry, by attempt number; a sixth failure is the last.
var Backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// The topics an endpoint may take besides TopicAny, as their packages emit them; a test holds them to events.Topics.
const (
	TopicRequirementCreated  = "requirement.created"
	TopicRequirementRevised  = "requirement.revised"
	TopicRequirementChanged  = "requirement.changed"
	TopicRequirementTrashed  = "requirement.trashed"
	TopicRequirementRestored = "requirement.restored"
	TopicRequirementMoved    = "requirement.moved"
	TopicLinkCreated         = "link.created"
	TopicLinkRemoved         = "link.removed"
	TopicLinkCleared         = "link.cleared"
	TopicReviewRequested     = "review.requested"
	TopicReviewDecided       = "review.decided"
	TopicReviewClosed        = "review.closed"
	TopicCommentCreated      = "comment.created"
	TopicThreadResolved      = "thread.resolved"
	TopicBaselineTaken       = "baseline.taken"
)

// Topics are the events an endpoint may take besides TopicAny, in the order the settings list them.
var Topics = []string{
	TopicRequirementCreated, TopicRequirementRevised, TopicRequirementChanged, TopicRequirementTrashed, TopicRequirementRestored,
	TopicRequirementMoved, TopicLinkCreated, TopicLinkRemoved, TopicLinkCleared, TopicReviewRequested, TopicReviewDecided,
	TopicReviewClosed, TopicCommentCreated, TopicThreadResolved, TopicBaselineTaken,
}

// Subscribable is every value an endpoint's topics may hold.
var Subscribable = append([]string{TopicAny}, Topics...)

// ReasonFailing is why the worker turned an endpoint off rather than a person.
const ReasonFailing = "failing"

// DisabledReasons lists them for the document.
var DisabledReasons = []string{ReasonFailing}

var (
	// ErrNotFound is an endpoint or delivery that is not here, or not the caller's to see.
	ErrNotFound = errors.New("there is no such webhook")
	// ErrNoKey refuses an endpoint whose secret could not be sealed.
	ErrNoKey = errors.New("webhooks need EXCITER_SECRET_KEY to keep their secrets")
	// ErrUnknownVersion is an envelope version this release does not send.
	ErrUnknownVersion = errors.New("there is no such webhook envelope version")
)

// FieldError is an input a person has to change.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// WebhookOwner is the administrator whose permissions an endpoint's payloads are read with: whoever saved it last.
type WebhookOwner struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// WebhookProject is a project an endpoint listens to.
type WebhookProject struct {
	ID   uuid.UUID `json:"id"`
	Key  string    `json:"key"`
	Name string    `json:"name"`
}

// Webhook is one address the installation posts to.
type Webhook struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	URL    string    `json:"url"`
	Topics []string  `json:"topics"`
	// Projects are the projects it listens to; empty is every project its owner may see.
	Projects []WebhookProject `json:"projects"`
	Enabled  bool             `json:"enabled"`
	// Version is the envelope version the endpoint is posted in: 1, or 2 for CloudEvents.
	Version int `json:"version"`
	// DisabledReason says the worker turned it off, and why; null when a person did or it is on.
	DisabledReason *string `json:"disabledReason"`
	// Failures counts the attempts that failed since the last delivered.
	Failures int `json:"failures"`
	// Owner is null once the administrator who saved it left; nothing but a ping is sent until somebody saves it again.
	WebhookOwner *WebhookOwner `json:"owner"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	// Secret is set only on the answer that made or rotated it.
	Secret string `json:"secret,omitempty"`
}

// DeliveryState is where a delivery stands.
type DeliveryState string

const (
	StatePending   DeliveryState = "pending"
	StateDelivered DeliveryState = "delivered"
	StateFailed    DeliveryState = "failed"
	// StateWithheld is an event whose requirement the owner may not view, or no longer exists: nothing was sent.
	StateWithheld DeliveryState = "withheld"
	// StateCancelled is an attempt that came due while the endpoint was off.
	StateCancelled DeliveryState = "cancelled"
)

// States lists them for the document.
var States = []DeliveryState{StatePending, StateDelivered, StateFailed, StateWithheld, StateCancelled}

// WebhookDelivery is one attempt at one event.
type WebhookDelivery struct {
	ID        uuid.UUID `json:"id"`
	WebhookID uuid.UUID `json:"webhookId"`
	// EventID is the envelope's id, the same on every attempt and redelivery, so a receiver can tell a repeat.
	EventID uuid.UUID `json:"eventId"`
	Topic   string    `json:"topic"`
	Attempt int       `json:"attempt"`
	// Manual is a test or a redelivery somebody asked for.
	Manual        bool          `json:"manual"`
	DeliveryState DeliveryState `json:"state"`
	// Status is what the receiver answered, null when it was not reached.
	Status        *int       `json:"status"`
	Error         string     `json:"error"`
	NextAttemptAt *time.Time `json:"nextAttemptAt"`
	AttemptedAt   *time.Time `json:"attemptedAt"`
	DeliveredAt   *time.Time `json:"deliveredAt"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// WebhookInput makes or changes an endpoint; enabled left out keeps it as it is, and a new endpoint starts on.
// Version left out is 1 for a new endpoint, and keeps the one a changed endpoint has.
type WebhookInput struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Topics []string `json:"topics"`
	// Projects are the keys of the projects it listens to; none is every project.
	Projects []string `json:"projects"`
	Enabled  *bool    `json:"enabled,omitempty"`
	Version  *int     `json:"version,omitempty"`
}

// Sign is the header value for a body: sha256= and the hex HMAC, Armature's and Stator's shape, so a receiver
// written for one serves all three.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify says whether a signature header matches the body, in constant time.
func Verify(body []byte, secret, header string) bool {
	return hmac.Equal([]byte(Sign(body, secret)), []byte(header))
}

// SignTimestamped signs "{timestamp}.{body}", the time in Unix seconds as the timestamp header carries it.
func SignTimestamped(body []byte, secret string, at time.Time) string {
	return Sign(append([]byte(strconv.FormatInt(at.Unix(), 10)+"."), body...), secret)
}

// VerifyTimestamped says whether the timestamped signature matches and the time it names is within tolerance of
// now; a replay outside that window fails.
func VerifyTimestamped(body []byte, secret, timestamp, header string, tolerance time.Duration, now time.Time) bool {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp {
		return false
	}
	at := time.Unix(seconds, 0)
	if drift := now.Sub(at); drift > tolerance || drift < -tolerance {
		return false
	}
	return hmac.Equal([]byte(SignTimestamped(body, secret, at)), []byte(header))
}

// Options are the settings of the process the service needs.
type Options struct {
	// AppURL is Exciter's own origin, for the links in a payload.
	AppURL string
	// Allow is what EXCITER_OUTBOUND_ALLOW names.
	Allow netguard.Allow
	Log   *slog.Logger
}

// Service keeps endpoints and deliveries and does the posting.
type Service struct {
	db     *db.Cluster
	box    *secret.Box
	client *http.Client
	opts   Options
	now    func() time.Time
}

// NewService returns the service; a nil box refuses to make an endpoint and fails every delivery, since no secret opens.
func NewService(cluster *db.Cluster, box *secret.Box, opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	opts.AppURL = strings.TrimSuffix(opts.AppURL, "/")
	return &Service{db: cluster, box: box, client: netguard.Client(sendTimeout, opts.Allow), opts: opts, now: time.Now}
}

// sealContext binds a sealed secret to its installation and endpoint, so a copy onto another row does not open.
func sealContext(installation, id uuid.UUID) []byte {
	return append(append([]byte("webhook:"), installation[:]...), id[:]...)
}

func newSecret() (string, error) {
	raw, _, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	return SecretPrefix + raw, nil
}
