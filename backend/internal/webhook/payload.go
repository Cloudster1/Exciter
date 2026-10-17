package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// EnvelopeV1 is what an endpoint on version 1 receives, the shape Armature and Stator send: the id is the event's,
// the same on every attempt, so a receiver can tell a repeat.
type EnvelopeV1 struct {
	Version    int       `json:"version"`
	ID         uuid.UUID `json:"id"`
	Source     string    `json:"source"`
	Topic      string    `json:"topic"`
	OccurredAt time.Time `json:"occurredAt"`
	Payload    any       `json:"payload"`
}

// EnvelopeV2 is what an endpoint on version 2 receives: a CloudEvent 1.0 in structured mode, its data version 1's
// payload and its id the event's, as in version 1.
type EnvelopeV2 struct {
	SpecVersion     string    `json:"specversion"`
	ID              uuid.UUID `json:"id"`
	Source          string    `json:"source"`
	Type            string    `json:"type"`
	Time            time.Time `json:"time"`
	DataContentType string    `json:"datacontenttype"`
	DataVersion     int       `json:"dataversion"`
	Data            any       `json:"data"`
}

// event is what every version's envelope is built from; source is the installation's address, version 2's source.
type event struct {
	id       uuid.UUID
	topic    string
	occurred time.Time
	payload  any
	source   string
}

// envelope builds one version's body and names its content type.
type envelope struct {
	build       func(e event) any
	contentType string
}

// envelopes are the versions an endpoint may be on. A later version is a row beside these, and an endpoint keeps
// getting the one it chose.
var envelopes = map[int]envelope{
	Version: {contentType: ContentTypeJSON, build: func(e event) any {
		return EnvelopeV1{Version: Version, ID: e.id, Source: Source, Topic: e.topic, OccurredAt: e.occurred, Payload: e.payload}
	}},
	VersionCloudEvents: {contentType: ContentTypeCloudEvents, build: func(e event) any {
		return EnvelopeV2{SpecVersion: SpecVersion, ID: e.id, Source: e.source, Type: TypePrefix + e.topic, Time: e.occurred,
			DataContentType: ContentTypeJSON, DataVersion: DataVersion, Data: e.payload}
	}},
}

// envelopeFor is the body an endpoint on the version is posted for the event.
func envelopeFor(version int, e event) ([]byte, error) {
	env, ok := envelopes[version]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, version)
	}
	return json.Marshal(env.build(e))
}

// contentType is what a body of the version is posted as.
func contentType(version int) string {
	if env, ok := envelopes[version]; ok {
		return env.contentType
	}
	return ContentTypeJSON
}

// source is what a version 2 envelope names as its sender: the installation's address, so a receiver hearing several
// tells them apart, and a path naming the product where no address is set.
func (s *Service) source() string {
	if s.opts.AppURL != "" {
		return s.opts.AppURL
	}
	return "/" + Source
}

// emitted is every field the outbox payloads of the topics carry, ids and what the event says, as their packages emit them.
type emitted struct {
	ActorID        uuid.UUID  `json:"actorId"`
	ProjectID      *uuid.UUID `json:"projectId"`
	RequirementID  *uuid.UUID `json:"requirementId"`
	LinkID         *uuid.UUID `json:"linkId"`
	SourceID       *uuid.UUID `json:"sourceId"`
	TargetID       *uuid.UUID `json:"targetId"`
	ArmatureLinkID *uuid.UUID `json:"armatureLinkId"`
	StatorLinkID   *uuid.UUID `json:"statorLinkId"`
	ReviewID       *uuid.UUID `json:"reviewId"`
	CommentID      *uuid.UUID `json:"commentId"`
	ThreadID       *uuid.UUID `json:"threadId"`
	BaselineID     *uuid.UUID `json:"baselineId"`
	Revision       int        `json:"revision"`
	Field          string     `json:"field"`
	FromKey        string     `json:"fromKey"`
	ToKey          string     `json:"toKey"`
	Decision       string     `json:"decision"`
	Signed         bool       `json:"signed"`
	Reply          bool       `json:"reply"`
	// Via is the token or connected app the change was written through, absent for a session (#190).
	Via *via.Via `json:"via"`
}

// requirements are the requirements an event is about, for the projects an endpoint listens to.
func (e emitted) requirements() []uuid.UUID {
	out := []uuid.UUID{}
	for _, id := range []*uuid.UUID{e.RequirementID, e.SourceID, e.TargetID} {
		if id != nil {
			out = append(out, *id)
		}
	}
	return out
}

// ProjectRef names a project.
type ProjectRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// StatusRef is a requirement's status and the category it stands in.
type StatusRef struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// RequirementRef names a requirement as its reader finds it, at its current revision.
type RequirementRef struct {
	ID       uuid.UUID  `json:"id"`
	Key      string     `json:"key"`
	Kind     string     `json:"kind"`
	Title    string     `json:"title"`
	Revision int        `json:"revision"`
	Status   *StatusRef `json:"status"`
	Project  ProjectRef `json:"project"`
	URL      string     `json:"url"`
	Trashed  bool       `json:"trashed"`
}

// Person is who acted; null when they are no longer a member.
type Person struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Via is the token or connected app they acted through, left out for a session, so a receiver of either
	// envelope version reads past it (#190).
	Via *via.Via `json:"via,omitempty"`
}

// RequirementEvent is the payload of requirement.created, requirement.trashed and requirement.restored.
type RequirementEvent struct {
	Requirement RequirementRef `json:"requirement"`
	Actor       *Person        `json:"actor"`
}

// RequirementRevised is the payload of requirement.revised: the revision the save made.
type RequirementRevised struct {
	Requirement RequirementRef `json:"requirement"`
	Revision    int            `json:"revision"`
	Actor       *Person        `json:"actor"`
}

// RequirementChanged is the payload of requirement.changed: what changed that makes no revision.
type RequirementChanged struct {
	Requirement RequirementRef `json:"requirement"`
	// Field is status, owner, priority, labels, place, key, link, attachment or trash.
	Field string  `json:"field"`
	Actor *Person `json:"actor"`
}

// RequirementMoved is the payload of requirement.moved: to another project, under a new key.
type RequirementMoved struct {
	Requirement RequirementRef `json:"requirement"`
	FromKey     string         `json:"fromKey"`
	ToKey       string         `json:"toKey"`
	Actor       *Person        `json:"actor"`
}

// LinkTypeRef is a link type and the phrases its ends read in.
type LinkTypeRef struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Forward  string `json:"forward"`
	Backward string `json:"backward"`
}

// LinkRef is a trace link; an end the owner may not view is null.
type LinkRef struct {
	ID     uuid.UUID       `json:"id"`
	Type   LinkTypeRef     `json:"type"`
	Source *RequirementRef `json:"source"`
	Target *RequirementRef `json:"target"`
}

// LinkEvent is the payload of link.created and link.removed.
type LinkEvent struct {
	Link  LinkRef `json:"link"`
	Actor *Person `json:"actor"`
}

// ClearedLink is the link a requirement's reviewer checked: a trace link, or one to an Armature issue or a Stator page.
type ClearedLink struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}

// The kinds of link a link.cleared names.
const (
	ClearedTrace    = "trace"
	ClearedArmature = "armature"
	ClearedStator   = "stator"
)

// ClearedKinds lists them for the document.
var ClearedKinds = []string{ClearedTrace, ClearedArmature, ClearedStator}

// LinkCleared is the payload of link.cleared: a suspect link checked against the requirement's revision.
type LinkCleared struct {
	Requirement RequirementRef `json:"requirement"`
	Link        ClearedLink    `json:"link"`
	Actor       *Person        `json:"actor"`
}

// ReviewRef is a review of one revision, in the state it is in now.
type ReviewRef struct {
	ID       uuid.UUID `json:"id"`
	Revision int       `json:"revision"`
	State    string    `json:"state"`
}

// ReviewEvent is the payload of review.requested and review.closed.
type ReviewEvent struct {
	Review      ReviewRef      `json:"review"`
	Requirement RequirementRef `json:"requirement"`
	Actor       *Person        `json:"actor"`
}

// ReviewDecided is the payload of review.decided: approve or reject, and whether it was an electronic signature (#46).
type ReviewDecided struct {
	Review      ReviewRef      `json:"review"`
	Requirement RequirementRef `json:"requirement"`
	Decision    string         `json:"decision"`
	Signed      bool           `json:"signed"`
	Actor       *Person        `json:"actor"`
}

// CommentRef is a comment's words, at most MaxCommentText characters of them.
type CommentRef struct {
	ID       uuid.UUID `json:"id"`
	ThreadID uuid.UUID `json:"threadId"`
	Reply    bool      `json:"reply"`
	Text     string    `json:"text"`
	URL      string    `json:"url"`
}

// CommentCreated is the payload of comment.created.
type CommentCreated struct {
	Comment     CommentRef     `json:"comment"`
	Requirement RequirementRef `json:"requirement"`
	Actor       *Person        `json:"actor"`
}

// ThreadRef is a discussion on a requirement.
type ThreadRef struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
}

// ThreadResolved is the payload of thread.resolved.
type ThreadResolved struct {
	Thread      ThreadRef      `json:"thread"`
	Requirement RequirementRef `json:"requirement"`
	Actor       *Person        `json:"actor"`
}

// BaselineRef names a baseline as it was taken.
type BaselineRef struct {
	ID      uuid.UUID  `json:"id"`
	Name    string     `json:"name"`
	Project ProjectRef `json:"project"`
	URL     string     `json:"url"`
}

// BaselineTaken is the payload of baseline.taken.
type BaselineTaken struct {
	Baseline BaselineRef `json:"baseline"`
	Actor    *Person     `json:"actor"`
}

// Ping is the payload of a test delivery.
type Ping struct {
	Message string `json:"message"`
}

// pingMessage is what a test delivery says.
const pingMessage = "Exciter can reach this address."

// body is the envelope for one attempt, its payload read as the endpoint's owner through the database's own rules:
// what they may not view is errWithheld, and no part of it is sent.
func (s *Service) body(ctx context.Context, c claimed) ([]byte, error) {
	if _, ok := envelopes[c.version]; !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, c.version)
	}
	env := event{id: c.eventID, topic: c.topic, occurred: c.occurred.UTC(), source: s.source()}
	if c.topic == TopicPing {
		env.payload = Ping{Message: pingMessage}
		return envelopeFor(c.version, env)
	}
	if c.owner == nil {
		return nil, errWithheld
	}
	var in emitted
	if err := json.Unmarshal(c.event, &in); err != nil {
		return nil, err
	}
	asOwner := db.PinPrimary(db.WithUser(ctx, *c.owner))
	err := s.db.Read(asOwner, func(ctx context.Context, tx db.DBTX) error {
		var err error
		env.payload, err = s.payload(ctx, tx, c.topic, in)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errWithheld
	}
	if err != nil {
		return nil, err
	}
	return envelopeFor(c.version, env)
}
