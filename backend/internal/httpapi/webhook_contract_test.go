package httpapi

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/stator"
)

// Exciter's side of the webhook contract (#103): the envelope each receiver documents reads the sender's, as its
// vendored document describes it, so a field Exciter relies on cannot leave or change there unnoticed.
func TestTheWebhookEnvelopesReadTheSendersOnes(t *testing.T) {
	ours := Spec()
	for _, c := range []struct {
		sender, document, path, header, theirs string
	}{
		{"Armature", "../../../api/armature/openapi.json", "/armature/webhook", armature.VersionHeader, "EnvelopeV1"},
		{"Stator", "../../../api/stator/openapi.json", "/stator/webhook", stator.VersionHeader, "WebhookEnvelope"},
	} {
		t.Run(c.sender, func(t *testing.T) {
			raw, err := os.ReadFile(c.document)
			if err != nil {
				t.Fatal(err)
			}
			var doc openapi.Document
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			theirs, err := doc.Resolve(&openapi.Schema{Ref: "#/components/schemas/" + c.theirs})
			if err != nil {
				t.Fatalf("%s's document has no %s: %v", c.sender, c.theirs, err)
			}
			op := ours.Paths[c.path]["post"]
			if op == nil || op.RequestBody == nil {
				t.Fatalf("POST %s documents no envelope", c.path)
			}
			if !slices.ContainsFunc(op.Parameters, func(p openapi.Parameter) bool { return p.In == "header" && p.Name == c.header }) {
				t.Errorf("POST %s does not document the %s header", c.path, c.header)
			}
			envelope, err := ours.Resolve(op.RequestBody.Content["application/json"].Schema)
			if err != nil {
				t.Fatal(err)
			}
			for name, field := range envelope.Properties {
				their, sent := theirs.Properties[name]
				switch {
				case !sent && slices.Contains(envelope.Required, name):
					t.Errorf("Exciter needs %q, which %s's %s does not have", name, c.sender, c.theirs)
				case sent && field.Type != nil && (field.Type != their.Type || field.Format != their.Format):
					t.Errorf("Exciter reads %q as %v %s, %s sends %v %s", name, field.Type, field.Format, c.sender, their.Type, their.Format)
				case sent && slices.Contains(envelope.Required, name) && !slices.Contains(theirs.Required, name):
					t.Errorf("Exciter needs %q, which %s may leave out", name, c.sender)
				}
			}
		})
	}
}
