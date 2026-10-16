package webhook

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const armaturePrefix = "dev.armature."

// Both of a partner's envelopes come out as one delivery, so a receiver acts the same on either.
func TestAPartnersDeliveryReadsTheSameInEitherVersion(t *testing.T) {
	id, org := uuid.New(), uuid.New()
	payload := `{"key":"PAY-7","toStatusCategory":"done"}`
	cases := []struct {
		name, header, body string
		version            int
		org                uuid.UUID
	}{
		{"version 1", "1", `{"version":1,"id":"` + id.String() + `","topic":"issue.transitioned","orgId":"` + org.String() +
			`","occurredAt":"2026-10-16T08:00:00Z","payload":` + payload + `}`, 1, org},
		{"a sender before versions", "", `{"id":"` + id.String() + `","topic":"issue.transitioned","occurredAt":"2026-10-16T08:00:00Z","payload":` +
			payload + `}`, 0, uuid.Nil},
		{"version 2", "2", `{"specversion":"1.0","id":"` + id.String() + `","source":"https://armature.example","type":"dev.armature.issue.transitioned",` +
			`"time":"2026-10-16T08:00:00Z","datacontenttype":"application/json","dataversion":1,"data":` + payload + `}`, 2, uuid.Nil},
		{"version 2 with its data version a string", "2", `{"specversion":"1.0","id":"` + id.String() + `","source":"/armature",` +
			`"type":"dev.armature.issue.transitioned","time":"2026-10-16T08:00:00Z","dataversion":"1","data":` + payload + `}`, 2, uuid.Nil},
		{"version 2 without a header", "", `{"specversion":"1.0","id":"` + id.String() + `","source":"/armature",` +
			`"type":"dev.armature.issue.transitioned","time":"2026-10-16T08:00:00Z","data":` + payload + `}`, 2, uuid.Nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Decode([]byte(c.body), c.header, armaturePrefix)
			if err != nil {
				t.Fatal(err)
			}
			if got.Version != c.version || got.ID != id || got.Topic != "issue.transitioned" || string(got.Payload) != payload || got.OrgID != c.org ||
				!got.OccurredAt.Equal(time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)) {
				t.Errorf("read as %+v", got)
			}
		})
	}
}

// A version Exciter does not read is refused wherever it is named, so the sender keeps the delivery instead of
// Exciter misreading it; a body that is no envelope at all is the receiver's to ignore.
func TestAVersionExciterDoesNotReadIsRefusedAndAStrangeBodyIgnored(t *testing.T) {
	id := uuid.New().String()
	ce := func(attrs string) string {
		return `{"id":"` + id + `","source":"/armature","type":"dev.armature.ping","time":"2026-10-16T08:00:00Z","data":{}` + attrs + `}`
	}
	refused := map[string][2]string{
		"a version 3 header":                   {"3", `{"version":1,"id":"` + id + `","topic":"ping"}`},
		"a version 3 body":                     {"", `{"version":3,"id":"` + id + `","topic":"ping"}`},
		"a version 1 body under a 2 header":    {"2", `{"version":1,"id":"` + id + `","topic":"ping"}`},
		"a CloudEvent under a 1 header":        {"1", ce(`,"specversion":"1.0"`)},
		"another CloudEvents version":          {"2", ce(`,"specversion":"2.0"`)},
		"a data version Exciter does not read": {"2", ce(`,"specversion":"1.0","dataversion":2`)},
	}
	for name, c := range refused {
		if _, err := Decode([]byte(c[1]), c[0], armaturePrefix); !errors.Is(err, ErrUnknownVersion) {
			t.Errorf("%s: %v, want an unknown version", name, err)
		}
	}
	ignored := map[string]string{
		"not JSON":                `<xml/>`,
		"no id":                   `{"version":1,"topic":"ping"}`,
		"a CloudEvent id no uuid": strings.Replace(ce(`,"specversion":"1.0"`), id, "event-7", 1),
	}
	for name, body := range ignored {
		if _, err := Decode([]byte(body), "", armaturePrefix); !errors.Is(err, ErrNotEnvelope) {
			t.Errorf("%s: %v, want a body that is no envelope", name, err)
		}
	}
	other, err := Decode([]byte(strings.Replace(ce(`,"specversion":"1.0"`), "dev.armature.", "dev.stator.", 1)), "2", armaturePrefix)
	if err != nil || other.Topic != "dev.stator.ping" {
		t.Errorf("another product's type read as %+v, %v; want it whole, so no topic matches", other, err)
	}
}
