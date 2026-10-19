package httpapi

import (
	"github.com/cloudster1/exciter/backend/internal/armature"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/version"
	"github.com/cloudster1/exciter/backend/internal/via"
	"github.com/cloudster1/exciter/backend/internal/webhook"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// ProductName is the document's title, which Armature and Stator check before
// they connect to it; /meta names the product as Product.
const ProductName = "Exciter"

// cloudEventsMediaType is the content type of a version 2 webhook envelope, CloudEvents' structured mode.
const cloudEventsMediaType = "application/cloudevents+json"

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
	b.Enums[reflect.TypeOf(auth.OrgRole(""))] = enumStrings(auth.OrgRoles)
	b.Enums[reflect.TypeOf(auth.Locale(""))] = enumStrings(auth.Locales)
	b.Enums[reflect.TypeOf(auth.Proof(""))] = []string{string(auth.ProofPassword), string(auth.ProofOIDC)}
	b.Enums[reflect.TypeOf(auth.TokenKind(""))] = enumStrings(auth.TokenKinds)
	b.Enums[reflect.TypeOf(via.Kind(""))] = enumStrings(via.Kinds)
	b.Enums[reflect.TypeOf(auth.RoleSource(""))] = enumStrings(auth.RoleSources)
	// A mapped group and an answered request make a member or an admin; ownership is the bootstrap's alone.
	granted := &openapi.Schema{Type: "string", Enum: []string{string(auth.RoleMember), string(auth.RoleAdmin)}}
	b.FieldOverrides["GroupRole.role"] = granted
	b.FieldOverrides["SetGroupRoleRequest.role"] = granted
	b.FieldOverrides["AdmitRequest.role"] = granted
	b.FieldOverrides["AuditEntry.action"] = &openapi.Schema{Type: "string", Enum: audit.Actions}
	b.Enums[reflect.TypeOf(webhook.DeliveryState(""))] = enumStrings(webhook.States)
	webhookTopics := &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: webhook.Subscribable}}
	b.FieldOverrides["Webhook.topics"] = webhookTopics
	b.FieldOverrides["WebhookInput.topics"] = webhookTopics
	b.FieldOverrides["WebhookInput.version"] = &openapi.Schema{Type: "integer",
		Description: "The envelope version the endpoint is posted in: " + webhook.VersionList() + ", 2 a CloudEvent. Left out, 1 for a new endpoint and unchanged for one saved before."}
	b.FieldOverrides["PartnerCloudEvent.specversion"] = &openapi.Schema{Type: "string", Enum: []string{webhook.SpecVersion}}
	b.FieldOverrides["PartnerCloudEvent.type"] = &openapi.Schema{Type: "string",
		Description: "The sender's prefix, dev.armature. or dev.stator., and the topic it names in version 1."}
	b.FieldOverrides["PartnerCloudEvent.time"] = &openapi.Schema{Type: "string", Format: "date-time"}
	b.FieldOverrides["PartnerCloudEvent.dataversion"] = &openapi.Schema{OneOf: []*openapi.Schema{{Type: "integer"}, {Type: "string"}},
		Description: "The version of the payload in data; Exciter reads 1, and its absence as 1."}
	b.FieldOverrides["PartnerCloudEvent.data"] = &openapi.Schema{Type: "object", Description: "The payload version 1 carries as its payload."}
	b.FieldOverrides["Webhook.disabledReason"] = &openapi.Schema{OneOf: []*openapi.Schema{{Type: "string", Enum: webhook.DisabledReasons}, {Type: "null"}}}
	b.FieldOverrides["WebhookDelivery.topic"] = &openapi.Schema{Type: "string", Enum: append([]string{webhook.TopicPing}, webhook.Topics...)}
	b.Enums[reflect.TypeOf(theme.Source(""))] = enumStrings(theme.Sources)
	b.FieldOverrides["AuditFacets.actions"] = &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: audit.Actions}}
	coreSchemas(b)
	reviewSchemas(b)
	traceabilitySchemas(b)
	documentSchemas(b)
	assistantSchemas(b)
	controlSchemas(b)
	evidenceSchemas(b)
	authoringSchemas(b)
	analysisSchemas(b)
	testSchemas(b)
	riskSchemas(b)
	b.Enums[reflect.TypeOf(search.ViewKind(""))] = enumStrings(search.ViewKinds)
	return b
}

// coreSchemas names the requirements core's types (#10), whose packages reuse
// short names such as CreateInput, and spells their enums.
func coreSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[project.CreateInput]():      "NewProject",
		reflect.TypeFor[project.UpdateInput]():      "ProjectUpdate",
		reflect.TypeFor[project.Settings]():         "ProjectSettings",
		reflect.TypeFor[project.SettingsInput]():    "ProjectSettingsInput",
		reflect.TypeFor[project.KeyInput]():         "ProjectKeyInput",
		reflect.TypeFor[project.KeyCheck]():         "ProjectKeyCheck",
		reflect.TypeFor[project.Template]():         "ProjectTemplate",
		reflect.TypeFor[perm.Grant]():               "ProjectGrant",
		reflect.TypeFor[perm.GrantInput]():          "ProjectGrantInput",
		reflect.TypeFor[perm.GrantsInput]():         "ProjectGrantsInput",
		reflect.TypeFor[requirement.CreateInput]():  "NewRequirement",
		reflect.TypeFor[requirement.UpdateInput]():  "RequirementUpdate",
		reflect.TypeFor[requirement.Can]():          "RequirementCan",
		reflect.TypeFor[requirement.Ref]():          "RequirementRef",
		reflect.TypeFor[requirement.TypeRef]():      "RequirementTypeRef",
		reflect.TypeFor[requirement.MoveInput]():    "MoveRequirement",
		reflect.TypeFor[requirement.CopyInput]():    "CopyRequirement",
		reflect.TypeFor[requirement.RestoreInput](): "RestoreRevision",
		reflect.TypeFor[requirement.Change]():       "RequirementChange",
		reflect.TypeFor[attribute.Type]():           "RequirementType",
		reflect.TypeFor[attribute.TypeInput]():      "RequirementTypeInput",
		reflect.TypeFor[attribute.Def]():            "Attribute",
		reflect.TypeFor[attribute.DefInput]():       "AttributeInput",
		reflect.TypeFor[attribute.KindInfo]():       "AttributeKind",
		reflect.TypeFor[attribute.Option]():         "AttributeOption",
		reflect.TypeFor[attribute.OptionInput]():    "AttributeOptionInput",
		reflect.TypeFor[workflow.Option]():          "RuleOption",
		reflect.TypeFor[workflow.Rule]():            "TransitionRule",
		reflect.TypeFor[workflow.RuleInput]():       "TransitionRuleInput",
		reflect.TypeFor[workflow.Point]():           "WorkflowPoint",
		reflect.TypeFor[workflow.Step]():            "WorkflowStep",
		reflect.TypeFor[workflow.StepInput]():       "WorkflowStepInput",
		reflect.TypeFor[workflow.GraphInput]():      "WorkflowInput",
		reflect.TypeFor[workflow.AssignInput]():     "WorkflowAssignment",
		reflect.TypeFor[workflow.Available]():       "AvailableTransition",
		reflect.TypeFor[trace.TypeRule]():           "LinkTypeRule",
		reflect.TypeFor[trace.Link]():               "TraceLink",
		reflect.TypeFor[trace.LinkInput]():          "TraceLinkInput",
		reflect.TypeFor[label.Input]():              "LabelInput",
		reflect.TypeFor[label.Suggestion]():         "LabelSuggestion",
		reflect.TypeFor[label.Labelled]():           "LabelledRequirement",
		reflect.TypeFor[search.Filter]():            "ViewFilter",
		reflect.TypeFor[search.Sort]():              "ViewSort",
		reflect.TypeFor[search.Row]():               "GridRow",
		reflect.TypeFor[search.Hit]():               "SearchHit",
		reflect.TypeFor[search.QuickHit]():          "QuickSearchHit",
		reflect.TypeFor[csvio.Column]():             "ImportColumn",
		reflect.TypeFor[csvio.Word]():               "ImportWord",
		reflect.TypeFor[csvio.PersonFound]():        "ImportPerson",
		reflect.TypeFor[csvio.Problem]():            "ImportProblem",
		reflect.TypeFor[csvio.Preview]():            "ImportPreview",
		reflect.TypeFor[csvio.Refusal]():            "ImportRefusal",
		reflect.TypeFor[csvio.PersonChoice]():       "ImportPersonChoice",
		reflect.TypeFor[csvio.MappingInput]():       "ImportMapping",
		reflect.TypeFor[reqifio.Import]():           "ReqIFImport",
		reflect.TypeFor[reqifio.Preview]():          "ReqIFPreview",
		reflect.TypeFor[reqifio.SchemaItem]():       "ReqIFSchemaItem",
		reflect.TypeFor[reqifio.Loss]():             "ReqIFLoss",
		reflect.TypeFor[reqifio.Problem]():          "ReqIFProblem",
		reflect.TypeFor[reqifio.Refusal]():          "ReqIFRefusal",
		reflect.TypeFor[reqifio.ChoicesInput]():     "ReqIFChoices",
		reflect.TypeFor[specdoc.Document]():         "SpecificationDocument",
		reflect.TypeFor[specdoc.GlossaryTerm]():     "SpecificationGlossaryTerm",
		reflect.TypeFor[specdoc.Entry]():            "DocumentEntry",
		reflect.TypeFor[specdoc.Property]():         "DocumentProperty",
		reflect.TypeFor[specdoc.Link]():             "DocumentLink",
		reflect.TypeFor[specdoc.BaselineInfo]():     "DocumentBaseline",
		reflect.TypeFor[armature.Connection]():      "ArmatureConnection",
		reflect.TypeFor[armature.ConnectionInput](): "ArmatureConnectionInput",
		reflect.TypeFor[armature.Account]():         "ArmatureAccount",
		reflect.TypeFor[armature.AccountUser]():     "ArmatureUser",
		reflect.TypeFor[armature.TokenInput]():      "ArmatureTokenInput",
		reflect.TypeFor[armature.Issue]():           "ArmatureIssue",
		reflect.TypeFor[armature.IssueStatus]():     "ArmatureIssueStatus",
		reflect.TypeFor[armature.LookupResult]():    "ArmatureLookup",
		reflect.TypeFor[armature.Link]():            "ArmatureLink",
		reflect.TypeFor[armature.LinkInput]():       "ArmatureLinkInput",
		reflect.TypeFor[armature.Project]():         "ArmatureProject",
		reflect.TypeFor[armature.IssueType]():       "ArmatureIssueType",
		reflect.TypeFor[armature.CreateInput]():     "ArmatureCreateInput",
		reflect.TypeFor[armature.Created]():         "ArmatureCreated",
		reflect.TypeFor[armature.CreateFailure]():   "ArmatureCreateFailure",
		reflect.TypeFor[armature.Follow]():          "ArmatureFollow",
		reflect.TypeFor[armature.Missing]():         "ArmatureMissing",
		reflect.TypeFor[stator.Connection]():        "StatorConnection",
		reflect.TypeFor[stator.ConnectionInput]():   "StatorConnectionInput",
		reflect.TypeFor[stator.Account]():           "StatorAccount",
		reflect.TypeFor[stator.AccountUser]():       "StatorUser",
		reflect.TypeFor[stator.TokenInput]():        "StatorTokenInput",
		reflect.TypeFor[stator.Page]():              "StatorPage",
		reflect.TypeFor[stator.LookupResult]():      "StatorLookup",
		reflect.TypeFor[stator.PageHit]():           "StatorPageHit",
		reflect.TypeFor[stator.Link]():              "StatorLink",
		reflect.TypeFor[stator.LinkInput]():         "StatorLinkInput",
		reflect.TypeFor[stator.ClearInput]():        "StatorClearInput",
		reflect.TypeFor[armature.WebhookEnvelope](): "ArmatureWebhookEnvelope",
		reflect.TypeFor[stator.WebhookEnvelope]():   "StatorWebhookEnvelope",
		reflect.TypeFor[webhook.CloudEvent]():       "PartnerCloudEvent",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(perm.SubjectType(""))] = enumStrings(perm.SubjectTypes)
	b.Enums[reflect.TypeOf(perm.Role(""))] = enumStrings(perm.Roles)
	b.Enums[reflect.TypeOf(perm.Right(""))] = enumStrings(perm.Rights)
	b.Enums[reflect.TypeOf(perm.StepKind(""))] = enumStrings(perm.StepKinds)
	b.Enums[reflect.TypeOf(requirement.Kind(""))] = enumStrings(requirement.Kinds)
	b.Enums[reflect.TypeOf(requirement.Priority(""))] = enumStrings(requirement.Priorities)
	b.Enums[reflect.TypeOf(requirement.DiffChange(""))] = enumStrings(requirement.DiffChanges)
	b.Enums[reflect.TypeOf(requirement.ActivityKind(""))] = enumStrings(requirement.ActivityKinds)
	b.Enums[reflect.TypeOf(requirement.ChangeField(""))] = enumStrings(requirement.ChangeFields)
	b.Enums[reflect.TypeOf(attribute.Kind(""))] = enumStrings(attribute.Kinds)
	b.Enums[reflect.TypeOf(workflow.Category(""))] = enumStrings(workflow.Categories)
	b.Enums[reflect.TypeOf(workflow.RuleKind(""))] = enumStrings(workflow.RuleKinds)
	b.Enums[reflect.TypeOf(workflow.OptionKind(""))] = enumStrings(workflow.OptionKinds)
	b.Enums[reflect.TypeOf(trace.SuspectEnd(""))] = enumStrings(trace.SuspectEnds)
	b.Enums[reflect.TypeOf(trace.Counts(""))] = enumStrings(trace.CountsValues)
	b.Enums[reflect.TypeOf(trace.Scope(""))] = enumStrings(trace.Scopes)
	b.Enums[reflect.TypeOf(trace.Direction(""))] = enumStrings(trace.Directions)
	b.Enums[reflect.TypeOf(search.Op(""))] = enumStrings(search.Ops)
	b.Enums[reflect.TypeOf(csvio.State(""))] = enumStrings(csvio.States)
	b.Enums[reflect.TypeOf(reqifio.State(""))] = enumStrings(reqifio.States)
	b.Enums[reflect.TypeOf(reqifio.Format(""))] = enumStrings(append(append([]reqifio.Format{}, reqifio.Formats...), reqifio.WordFormats...))
	b.Enums[reflect.TypeOf(reqifio.SchemaKind(""))] = enumStrings(reqifio.SchemaKinds)
	b.Enums[reflect.TypeOf(reqifio.MatchBy(""))] = enumStrings(reqifio.MatchBys)
	b.Enums[reflect.TypeOf(reqifio.LossKind(""))] = enumStrings(reqifio.LossKinds)
	b.Enums[reflect.TypeOf(specdoc.Kind(""))] = enumStrings(specdoc.Kinds)
	b.Enums[reflect.TypeOf(armature.Status(""))] = enumStrings(armature.Statuses)
	b.Enums[reflect.TypeOf(armature.Relation(""))] = enumStrings(armature.Relations)
	b.Enums[reflect.TypeOf(armature.SyncState(""))] = enumStrings(armature.SyncStates)
	// Null until Armature is asked, which counts as offering everything.
	b.FieldOverrides["ArmatureConnection.capabilities"] = openapi.Nullable(&openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string"}})
	b.Enums[reflect.TypeOf(stator.Status(""))] = enumStrings(stator.Statuses)
	b.Enums[reflect.TypeOf(stator.Relation(""))] = enumStrings(stator.Relations)
	b.Enums[reflect.TypeOf(stator.SyncState(""))] = enumStrings(stator.SyncStates)

	category := &openapi.Schema{Type: "string", Enum: enumStrings(workflow.Categories)}
	b.FieldOverrides["StatusRef.category"] = category
	b.FieldOverrides["ViewFilter.categories"] = &openapi.Schema{Type: "array", Items: category}
	b.FieldOverrides["ViewFilter.priorities"] = &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: enumStrings(requirement.Priorities)}}
	b.FieldOverrides["APIToken.scopes"] = &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: []string{auth.ScopeRead, auth.ScopeEvidence}}}
	b.FieldOverrides["NewAPIToken.scopes"] = &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: []string{auth.ScopeRead, auth.ScopeEvidence}}}
	// A requirement update clears an owner, a priority or a rationale with null, and leaves out what it keeps.
	b.FieldOverrides["RequirementUpdate.ownerId"] = openapi.Nullable(&openapi.Schema{Type: "string", Format: "uuid"})
	b.FieldOverrides["RequirementUpdate.priority"] = openapi.Nullable(&openapi.Schema{Type: "string", Enum: enumStrings(requirement.Priorities)})
	b.FieldOverrides["RequirementUpdate.rationale"] = &openapi.Schema{Description: "A document of the shared editor, or null to clear it."}
}

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := specBuilder()

	doc := &openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:   ProductName,
			Version: APIVersion,
			Description: "The requirements management HTTP API. Every endpoint answers JSON; errors share one envelope. " +
				"Sign in with a session cookie or an API token sent as a bearer token.",
		},
		Servers: []openapi.Server{{URL: APIPrefix, Description: "This deployment"}},
		Paths:   map[string]openapi.PathItem{},
		Components: openapi.Components{
			SecuritySchemes: map[string]openapi.SecurityScheme{
				"session": {Type: "apiKey", In: "cookie", Name: config.DefaultSessionCookie, Description: "The cookie a sign-in sets."},
			},
		},
		Security: []map[string][]string{{"session": {}}},
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
	doc.Webhooks = map[string]openapi.PathItem{"event": {"post": webhookDelivery(b)}}
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
	for _, h := range op.headers {
		o.Parameters = append(o.Parameters, openapi.Parameter{Name: h.name, In: "header", Description: h.description, Schema: h.schema})
	}
	switch {
	case op.multipart:
		o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
			"multipart/form-data": {Schema: multipartSchema(b, op)},
		}}
	case op.form:
		o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
			"application/x-www-form-urlencoded": {Schema: b.SchemaOf(op.request)},
		}}
	case op.request != nil:
		o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
			"application/json": {Schema: b.SchemaOf(op.request)},
		}}
	}
	if op.cloudEvent && o.RequestBody != nil {
		o.RequestBody.Content[cloudEventsMediaType] = openapi.MediaType{Schema: b.SchemaOf(webhook.CloudEvent{})}
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
	if op.redirect {
		o.Responses["302"] = &openapi.Response{Description: "Found: the browser is sent on."}
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
	if op.root {
		o.Servers = []openapi.Server{{URL: "/", Description: "Outside the API's prefix"}}
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

// multipartSchema is a form with a part named file, once or more, beside the request's own fields.
func multipartSchema(b *openapi.Builder, op operation) *openapi.Schema {
	file := &openapi.Schema{Type: "string", Format: "binary"}
	if op.files {
		file = &openapi.Schema{Type: "array", Items: file}
	}
	form := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	if op.request != nil {
		form = b.Inline(op.request)
	}
	form.Properties["file"] = file
	form.Required = append(form.Required, "file")
	return form
}

func ok(body any) map[int]any      { return map[int]any{http.StatusOK: body} }
func created(body any) map[int]any { return map[int]any{http.StatusCreated: body} }
func none() map[int]any            { return map[int]any{http.StatusNoContent: nil} }

// enumStrings spells a named string type's values for a schema.
func enumStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// Route is one operation as the integration suite sees it: which request it
// answers, and whether it is built yet.
type Route struct {
	Method, Path, ID string
	Public, Binary   bool
	// Redirect operations answer by sending the browser on, a refusal included.
	Redirect bool
	// Pending operations answer 501 until they are built.
	Pending bool
	// OrgWide operations are refused a token limited to projects.
	OrgWide bool
}

// Catalog lists every operation in the table.
func Catalog() []Route {
	out := make([]Route, 0, len(operations))
	for _, op := range operations {
		out = append(out, Route{Method: op.method, Path: op.path, ID: op.operationID(), Public: op.public, Binary: op.binary, Redirect: op.redirect, Pending: op.pending, OrgWide: op.orgWide})
	}
	return out
}

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
