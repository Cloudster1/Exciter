package armature

import (
	"errors"
	"slices"
)

// What Exciter needs of Armature, by the names Armature lists among its capabilities on GET /meta (#100).
const (
	CapabilityIssues      = "issue-embed@1"
	CapabilityRemoteLinks = "remote-links@1"
	CapabilityWebhooks    = "webhooks@1"
	CapabilityThemes      = "theme-export@1"
)

// CapabilityLinkRelations lets a remote link say it is a requirement (#297); without it a link is a page, so it
// is sent only to an Armature that lists it, and is not among what Exciter needs.
const CapabilityLinkRelations = "remote-link-relations@1"

// needs says what Exciter cannot do without each capability, in the words of the sentence that says so.
var needs = []struct{ capability, feature string }{
	{CapabilityIssues, "show and find its issues on requirements"},
	{CapabilityRemoteLinks, "show on an issue the requirements traced to it"},
	{CapabilityWebhooks, "keep the statuses of linked issues current"},
	{CapabilityThemes, "follow the theme Armature shows each person"},
}

// Needs lists the capabilities Exciter checks for, for the API document and the tests.
func Needs() []string {
	out := make([]string, 0, len(needs))
	for _, n := range needs {
		out = append(out, n.capability)
	}
	return out
}

// ErrLacks is an Armature that does not offer a capability the feature needs; the Lacking error says which.
var ErrLacks = errors.New("Armature does not offer what the feature needs")

// Missing is a capability the connected Armature does not list, and what Exciter cannot do without it.
type Missing struct {
	Capability string `json:"capability"`
	Message    string `json:"message"`
}

func (m *Missing) Error() string { return m.Message }
func (m *Missing) Unwrap() error { return ErrLacks }

// Offered is what Armature listed when last asked. Nil is not asked yet, which counts as offering everything, so
// upgrading Exciter alone switches nothing off.
type Offered []string

// Offers says whether the capability is listed, by its exact name@version.
func (o Offered) Offers(capability string) bool { return o == nil || slices.Contains(o, capability) }

// Lacks is nil when Armature offers the capability, else why the feature that needs it cannot run.
func (o Offered) Lacks(capability string) *Missing {
	if o.Offers(capability) {
		return nil
	}
	for _, n := range needs {
		if n.capability == capability {
			return &Missing{Capability: capability, Message: "The connected Armature does not offer " + capability +
				", which Exciter needs to " + n.feature + ". Update Armature to a release that offers it, or ask its administrator to."}
		}
	}
	return &Missing{Capability: capability, Message: "The connected Armature does not offer " + capability + ". Update Armature to a release that offers it, or ask its administrator to."}
}

// Missing lists every capability Exciter needs that Armature does not offer, in the order of needs.
func (o Offered) Missing() []Missing {
	out := []Missing{}
	for _, n := range needs {
		if m := o.Lacks(n.capability); m != nil {
			out = append(out, *m)
		}
	}
	return out
}
