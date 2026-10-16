package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Armature's and Stator's deliveries to Exciter, read the same way in either envelope version, so each receiver acts
// on one shape whatever its sender was registered for.

// ErrNotEnvelope is a signed body that is neither envelope; a receiver ignores it.
var ErrNotEnvelope = errors.New("the body is not a webhook envelope")

// CloudEvent is a partner's version 2 envelope as Exciter reads it: the CloudEvents attributes Exciter needs, its data
// the version 1 payload.
type CloudEvent struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	DataVersion     json.RawMessage `json:"dataversion,omitempty"`
	Data            json.RawMessage `json:"data"`
}

// Inbound is a partner's delivery in either version, the fields its receiver acts on.
type Inbound struct {
	// Version is 0 for a sender from before versions, as its header and body say.
	Version    int
	ID         uuid.UUID
	Topic      string
	OccurredAt time.Time
	Payload    json.RawMessage
	// OrgID is Armature's deprecated organization, which only version 1 carries.
	OrgID uuid.UUID
}

// inboundV1 is version 1 as both partners send it; Armature adds orgId while it is deprecated.
type inboundV1 struct {
	Version    int             `json:"version"`
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	OrgID      uuid.UUID       `json:"orgId"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

// Decode reads a delivery a sender's secret vouched for. The header and the body have to name one version Exciter
// reads, or the error is ErrUnknownVersion and the sender keeps the delivery; typePrefix leads the sender's types.
func Decode(body []byte, header, typePrefix string) (*Inbound, error) {
	if !ReadsHeader(header) {
		return nil, fmt.Errorf("%w: %s in the header", ErrUnknownVersion, header)
	}
	var shape struct {
		SpecVersion *string `json:"specversion"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEnvelope, err)
	}
	if shape.SpecVersion != nil {
		return decodeCloudEvent(body, header, typePrefix)
	}
	var v1 inboundV1
	if err := json.Unmarshal(body, &v1); err != nil || v1.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEnvelope, err)
	}
	if v1.Version != 0 && v1.Version != Version || header != "" && header != strconv.Itoa(Version) {
		return nil, fmt.Errorf("%w: %d in the body, %q in the header", ErrUnknownVersion, v1.Version, header)
	}
	return &Inbound{Version: v1.Version, ID: v1.ID, Topic: v1.Topic, OccurredAt: v1.OccurredAt, Payload: v1.Payload, OrgID: v1.OrgID}, nil
}

func decodeCloudEvent(body []byte, header, typePrefix string) (*Inbound, error) {
	var ce CloudEvent
	if err := json.Unmarshal(body, &ce); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEnvelope, err)
	}
	if ce.SpecVersion != SpecVersion || header != "" && header != strconv.Itoa(VersionCloudEvents) || !readsDataVersion(ce.DataVersion) {
		return nil, fmt.Errorf("%w: specversion %q, dataversion %s, %q in the header", ErrUnknownVersion, ce.SpecVersion, ce.DataVersion, header)
	}
	id, err := uuid.Parse(ce.ID)
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("%w: the id %q is not the event's", ErrNotEnvelope, ce.ID)
	}
	// Another product's type stays whole, no topic of this sender's, so the receiver ignores it as any it does not know.
	topic, _ := strings.CutPrefix(ce.Type, typePrefix)
	return &Inbound{Version: VersionCloudEvents, ID: id, Topic: topic, OccurredAt: ce.Time, Payload: ce.Data}, nil
}

// readsDataVersion says whether the data is the payload Exciter reads: version 1, as a number or, as CloudEvents
// bindings other than JSON carry an extension, a string; absent is version 1 too.
func readsDataVersion(raw json.RawMessage) bool {
	switch string(bytes.TrimSpace(raw)) {
	case "", "null", strconv.Itoa(DataVersion), strconv.Quote(strconv.Itoa(DataVersion)):
		return true
	}
	return false
}
