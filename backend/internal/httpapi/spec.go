package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/version"
)

// ProductName is what the document and /meta call this API, which Armature and
// Stator check before they connect to it.
const ProductName = "Exciter"

// isErrorEnvelope marks a refusal a client has to tell apart, listed with its
// status next to the successes.
func isErrorEnvelope(body any) bool {
	_, is := body.(errorEnvelope)
	return is
}

// specBuilder is the builder with every override the table relies on, shared
// by the document and the MCP tools so both describe a type alike.
func specBuilder() *openapi.Builder {
	b := openapi.NewBuilder()
	b.Names[reflect.TypeOf(version.Info{})] = "BuildInfo"
	b.FieldOverrides["AuditEntry.action"] = &openapi.Schema{Type: "string", Enum: audit.Actions}
	b.FieldOverrides["AuditFacets.actions"] = &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: audit.Actions}}
	return b
}

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := specBuilder()

	doc := &openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:   ProductName,
			Version: "1",
			Description: "The requirements management HTTP API. Every endpoint answers JSON; errors share one envelope. " +
				"Sign in with a session cookie or an API token sent as a bearer token.",
		},
		Servers: []openapi.Server{{URL: APIPrefix, Description: "This deployment"}},
		Paths:   map[string]openapi.PathItem{},
	}

	errorSchema := b.Schema(reflect.TypeOf(errorEnvelope{}))
	tags := map[string]bool{}
	for _, op := range operations {
		item := doc.Paths[op.path]
		if item == nil {
			item = openapi.PathItem{}
			doc.Paths[op.path] = item
		}
		item[strings.ToLower(op.method)] = op.describe(b, errorSchema)
		tags[op.tag] = true
	}
	for _, tag := range openapi.SortedKeys(tags) {
		doc.Tags = append(doc.Tags, openapi.Tag{Name: tag})
	}
	doc.Components.Schemas = b.Components()
	return doc
}

// describe is one row of the table as an OpenAPI operation.
func (op operation) describe(b *openapi.Builder, errorSchema *openapi.Schema) *openapi.Operation {
	o := &openapi.Operation{
		OperationID: op.operationID(),
		Summary:     op.summary,
		Description: op.description,
		Tags:        []string{op.tag},
		Responses:   map[string]*openapi.Response{},
	}
	for _, name := range pathParams(op.path) {
		o.Parameters = append(o.Parameters, openapi.Parameter{
			Name: name, In: "path", Required: true, Schema: pathParamSchema(name),
		})
	}
	for _, q := range op.query {
		schema := q.schema
		if schema == nil {
			schema = &openapi.Schema{Type: "string"}
		}
		if q.repeated {
			schema = &openapi.Schema{Type: "array", Items: schema}
		}
		o.Parameters = append(o.Parameters, openapi.Parameter{Name: q.name, In: "query", Description: q.description, Schema: schema})
	}
	if op.request != nil {
		o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
			"application/json": {Schema: b.SchemaOf(op.request)},
		}}
	}
	for status, body := range op.responses {
		r := &openapi.Response{Description: http.StatusText(status)}
		switch {
		case isErrorEnvelope(body):
			r.Content = map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}
		case op.binary && status == http.StatusOK:
			r.Content = map[string]openapi.MediaType{"*/*": {Schema: &openapi.Schema{Type: "string", Format: "binary"}}}
		case body != nil:
			r.Content = map[string]openapi.MediaType{"application/json": {Schema: envelopeSchema(b, body)}}
		}
		o.Responses[strconv.Itoa(status)] = r
	}
	if op.pending {
		o.Responses["501"] = &openapi.Response{Description: "Not Implemented: agreed, not built yet.",
			Content: map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}}
	}
	o.Responses["default"] = &openapi.Response{Description: "An error, in the one shape every endpoint uses.",
		Content: map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}}
	if op.public {
		o.Security = []map[string][]string{}
	}
	return o
}

// operationID is what a client calls the operation: the handler's name unless
// the table says otherwise.
func (op operation) operationID() string {
	if op.id != "" {
		return op.id
	}
	id := strings.TrimPrefix(op.handler, "handle")
	return strings.ToLower(id[:1]) + id[1:]
}

// envelopeSchema describes a response body: an env becomes an inline object
// with every key required, anything else is the type itself.
func envelopeSchema(b *openapi.Builder, body any) *openapi.Schema {
	e, ok := body.(env)
	if !ok {
		return b.SchemaOf(body)
	}
	s := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	for _, key := range openapi.SortedKeys(e) {
		schema := b.SchemaOf(e[key])
		if schema == nil {
			schema = &openapi.Schema{Description: "A JSON value."}
		}
		s.Properties[key] = schema
		s.Required = append(s.Required, key)
	}
	return s
}

var pathParamPattern = regexp.MustCompile(`\{(\w+)\}`)

func pathParams(path string) []string {
	var out []string
	for _, m := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

// pathParamSchema types a path parameter by its name: ids are uuids, numbers
// integers, the rest strings.
func pathParamSchema(name string) *openapi.Schema {
	switch {
	case strings.HasSuffix(name, "ID"):
		return &openapi.Schema{Type: "string", Format: "uuid"}
	case strings.HasSuffix(name, "Number"):
		return &openapi.Schema{Type: "integer"}
	}
	return &openapi.Schema{Type: "string"}
}

func ok(body any) map[int]any { return map[int]any{http.StatusOK: body} }

// Route is one operation as the integration suite sees it: which request it
// answers, and whether it is built yet.
type Route struct {
	Method, Path, ID string
	Public, Binary   bool
	// Pending operations answer 501 until they are built.
	Pending bool
	// OrgWide operations are refused a token limited to projects.
	OrgWide bool
}

// Catalog lists every operation in the table.
func Catalog() []Route {
	out := make([]Route, 0, len(operations))
	for _, op := range operations {
		out = append(out, Route{Method: op.method, Path: op.path, ID: op.operationID(), Public: op.public, Binary: op.binary, Pending: op.pending, OrgWide: op.orgWide})
	}
	return out
}

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
