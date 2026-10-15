package httpapi

import (
	"reflect"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// webhookPayloads are the payloads an endpoint may be posted, each named in the document as Webhook and its own name,
// so none takes the name of a type the answers use.
var webhookPayloads = []any{
	webhook.RequirementEvent{}, webhook.RequirementRevised{}, webhook.RequirementChanged{}, webhook.RequirementMoved{},
	webhook.LinkEvent{}, webhook.LinkCleared{}, webhook.ReviewEvent{}, webhook.ReviewDecided{}, webhook.CommentCreated{},
	webhook.ThreadResolved{}, webhook.BaselineTaken{}, webhook.Ping{},
}

// webhookDelivery is what an endpoint is posted for each event, in Stator's shape: a receiver is written against it,
// so the body and the headers it is signed in are here.
func webhookDelivery(b *openapi.Builder) *openapi.Operation {
	for _, v := range append(append([]any{}, webhookPayloads...), webhook.ProjectRef{}, webhook.StatusRef{}, webhook.RequirementRef{},
		webhook.Person{}, webhook.LinkTypeRef{}, webhook.LinkRef{}, webhook.ClearedLink{}, webhook.ReviewRef{}, webhook.CommentRef{},
		webhook.ThreadRef{}, webhook.BaselineRef{}) {
		t := reflect.TypeOf(v)
		b.Names[t] = "Webhook" + t.Name()
	}
	b.Names[reflect.TypeOf(webhook.EnvelopeV1{})] = "WebhookEnvelope"
	b.Descriptions[reflect.TypeOf(webhook.EnvelopeV1{})] = "What an endpoint registered for envelope version 1 is posted."
	b.FieldOverrides["WebhookEnvelope.version"] = &openapi.Schema{Type: "integer", Description: "The envelope version, 1 here; the version header says the same."}
	b.FieldOverrides["WebhookEnvelope.source"] = &openapi.Schema{Type: "string", Enum: []string{webhook.Source}, Description: "The product that sent the delivery, always " + webhook.Source + "."}
	b.FieldOverrides["WebhookEnvelope.topic"] = &openapi.Schema{Type: "string",
		Description: "What happened: " + strings.Join(append([]string{webhook.TopicPing}, webhook.Topics...), ", ") + ". A receiver ignores a topic it does not know."}
	b.FieldOverrides["WebhookEnvelope.occurredAt"] = &openapi.Schema{Type: "string", Format: "date-time", Description: "When the event happened, in UTC."}
	b.FieldOverrides["WebhookClearedLink.kind"] = &openapi.Schema{Type: "string", Enum: webhook.ClearedKinds}
	payloads := []*openapi.Schema{}
	for _, p := range webhookPayloads {
		payloads = append(payloads, b.SchemaOf(p))
	}
	b.FieldOverrides["WebhookEnvelope.payload"] = &openapi.Schema{AnyOf: payloads,
		Description: "The event's own fields, by topic, read as the endpoint's owner when the attempt is sent."}
	header := func(name, description string) openapi.Parameter {
		return openapi.Parameter{Name: name, In: "header", Required: true, Description: description, Schema: &openapi.Schema{Type: "string"}}
	}
	return &openapi.Operation{
		OperationID: "webhookEvent",
		Summary:     "What a webhook endpoint is posted for each event it subscribes to.",
		Description: "A receiver ignores fields and topics it does not know, and refuses an envelope version it does not know. " +
			"The version rises only for a change that breaks a receiver, and an endpoint keeps the version it was registered for.",
		Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			header(webhook.EventHeader, "The topic, as in the body."),
			header(webhook.DeliveryHeader, "This attempt's id; a retry or a redelivery has its own."),
			header(webhook.VersionHeader, "The envelope version of the body, as in its version field."),
			header(webhook.SignatureHeader, "sha256= and the hex HMAC-SHA256 of the body, keyed with the endpoint's secret."),
			header(webhook.TimestampHeader, "When this attempt was sent, in Unix seconds."),
			header(webhook.TimestampedSignatureHeader, "sha256= and the hex HMAC-SHA256 of the timestamp, a dot and the body."),
		},
		RequestBody: &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
			"application/json": {Schema: b.SchemaOf(webhook.EnvelopeV1{})},
		}},
		Responses: map[string]*openapi.Response{
			"2XX":     {Description: "Taken: the delivery is done."},
			"default": {Description: "Not taken: the delivery is tried again later, with backoff, as is one not answered in time."},
		},
		Security: []map[string][]string{},
	}
}
