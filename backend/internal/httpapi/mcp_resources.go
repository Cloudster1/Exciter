package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// The resources an assistant reads whole (#187): the API document, and a specification, a requirement or a baseline
// as Markdown, each read through the in-process call it stands for, as the caller, as a tool call is.
const (
	// MCPMaxResourceBytes bounds one resource read; a longer document is cut between two entries.
	MCPMaxResourceBytes = 256 << 10
	// MCPResourcePageSize is how many resources one resources/list page holds.
	MCPResourcePageSize = 50
	mcpOpenAPIResource  = "exciter://openapi.json"
	// rpcResourceNotFound answers an address that names nothing the caller may read, as invalid params since the
	// protocol's SEP-2164, where it had been -32002.
	rpcResourceNotFound = rpcInvalidParams
	markdownMIME        = "text/markdown"
)

var resourceTemplates = []map[string]any{
	{"uriTemplate": "exciter://projects/{projectKey}/spec{?section}", "name": "specification", "title": "A project's specification",
		"mimeType": markdownMIME, "description": "The project's specification as one Markdown document, each entry a heading with its " +
			"outline, key and title, then its fields, text and links; with section, the requirement or section of that key and what is below it."},
	{"uriTemplate": "exciter://requirements/{reqKey}", "name": "requirement", "title": "A requirement",
		"mimeType": markdownMIME, "description": "One requirement or section and what is below it, as its project's specification writes them."},
	{"uriTemplate": "exciter://baselines/{baselineID}/spec", "name": "baseline", "title": "A baseline",
		"mimeType": markdownMIME, "description": "A baseline's specification as it was taken, each requirement at the revision it holds."},
}

// resourceRead is what a resource's address names.
type resourceRead struct {
	host, key, section string
}

const (
	hostProjects     = "projects"
	hostRequirements = "requirements"
	hostBaselines    = "baselines"
)

// parseResource reads an address of the templates above; ok is false for any other.
func parseResource(uri string) (resourceRead, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != MCPServerName || u.User != nil || u.Fragment != "" {
		return resourceRead{}, false
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	read := resourceRead{host: u.Host, key: segments[0], section: u.Query().Get("section")}
	if read.key == "" || (u.RawQuery != "" && (u.Host != hostProjects || len(u.Query()) != 1 || read.section == "")) {
		return resourceRead{}, false
	}
	switch {
	case u.Host == hostProjects && len(segments) == 2 && segments[1] == "spec":
	case u.Host == hostBaselines && len(segments) == 2 && segments[1] == "spec":
	case u.Host == hostRequirements && len(segments) == 1:
	default:
		return resourceRead{}, false
	}
	return read, true
}

func (s *Server) listResources(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(params, &p)
	start := 0
	if p.Cursor != "" {
		n, err := strconv.Atoi(p.Cursor)
		if err != nil || n < 0 {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "The cursor is not one resources/list gave. Start again without it."}
		}
		start = n
	}
	all := []map[string]any{{"uri": mcpOpenAPIResource, "name": "openapi.json", "title": "The HTTP API", "mimeType": "application/json",
		"description": "The whole HTTP API, of which the tools are a part."}}
	var found struct {
		Projects []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := s.fetch(r, "/projects", nil, &found); err != nil {
		return nil, &err.rpcError
	}
	for _, p := range found.Projects {
		all = append(all, map[string]any{"uri": "exciter://projects/" + p.Key + "/spec", "name": p.Key, "title": p.Name, "mimeType": markdownMIME,
			"description": "The specification of " + p.Name + " as one numbered Markdown document."})
	}
	end := min(start+MCPResourcePageSize, len(all))
	out := map[string]any{"resources": all[min(start, end):end]}
	if end < len(all) {
		out["nextCursor"] = strconv.Itoa(end)
	}
	return out, nil
}

func (s *Server) readResource(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "A read names the resource by its uri."}
	}
	if p.URI == mcpOpenAPIResource {
		text, err := Spec().MarshalIndent()
		if err != nil {
			return nil, &rpcError{Code: rpcInternalError, Message: "The document could not be written."}
		}
		return resourceContents(p.URI, "application/json", string(text)), nil
	}
	text, rpcErr := s.resourceText(r, p.URI)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return resourceContents(p.URI, markdownMIME, text), nil
}

func resourceContents(uri, mime, text string) map[string]any {
	return map[string]any{"contents": []map[string]any{{"uri": uri, "mimeType": mime, "text": text}}}
}

// resourceText reads a document a resource names as Markdown, cut to what one read holds.
func (s *Server) resourceText(r *http.Request, uri string) (string, *rpcError) {
	read, ok := parseResource(uri)
	if !ok {
		return "", &rpcError{Code: rpcInvalidParams,
			Message: "There is no resource " + uri + ". Ask resources/list and resources/templates/list for the addresses."}
	}
	projectKey := read.key
	path := "/baselines/" + url.PathEscape(read.key) + "/document"
	if read.host == hostRequirements {
		// A requirement is its project's document narrowed to it, so its project is read first.
		var found struct {
			Requirement struct {
				ProjectKey string `json:"projectKey"`
			} `json:"requirement"`
		}
		if err := s.fetch(r, "/requirements/"+url.PathEscape(read.key), nil, &found); err != nil {
			return "", err.about(uri)
		}
		projectKey, read.section = found.Requirement.ProjectKey, read.key
	}
	query := url.Values{}
	if read.host != hostBaselines {
		path = "/projects/" + url.PathEscape(projectKey) + "/document"
		if read.section != "" {
			query.Set("section", read.section)
		}
	}
	var got struct {
		Document specdoc.Document `json:"document"`
	}
	if err := s.fetch(r, path, query, &got); err != nil {
		return "", err.about(uri)
	}
	return cut(got.Document, got.Document.Markdown()), nil
}

// cut holds a document to MCPMaxResourceBytes, ending after the last entry that fits with a sentence on how to read on.
func cut(doc specdoc.Document, md specdoc.Markdown) string {
	if whole := md.String(); len(whole) <= MCPMaxResourceBytes {
		return whole
	}
	// Counted as Upto joins the parts, with room for a sentence like the closing one, which is checked once written.
	const separator = len("\n\n")
	size, held := len(md.Head)+len("\n")+separator+len(cutSentence(doc, len(doc.Entries)-1)), 0
	for held < len(md.Entries) && size+len(md.Entries[held])+separator <= MCPMaxResourceBytes {
		size += len(md.Entries[held]) + separator
		held++
	}
	out := md.Upto(held, cutSentence(doc, held))
	for held > 0 && len(out) > MCPMaxResourceBytes {
		held--
		out = md.Upto(held, cutSentence(doc, held))
	}
	return out
}

// cutSentence says where a document was cut and how to read what comes after it.
func cutSentence(doc specdoc.Document, held int) string {
	next := doc.Entries[held]
	if held == 0 {
		return fmt.Sprintf("%s alone is larger than one read holds. Read it with get_requirement.", next.Key)
	}
	if doc.Kind == specdoc.KindBaseline {
		return fmt.Sprintf("The document was cut here, after %d entries. Read the rest with get_baseline_spec and offset %d.", held, held)
	}
	return fmt.Sprintf("The document was cut here, after %d entries. Read on with exciter://projects/%s/spec?section=%s, the next "+
		"entry and what is below it.", held, doc.ProjectKey, next.Key)
}

// fetch makes the GET a resource stands for, as the caller, through the whole router, and reads its answer.
func (s *Server) fetch(r *http.Request, path string, query url.Values, into any) *resourceError {
	if s.handler == nil {
		return &resourceError{rpcError{Code: rpcInternalError, Message: "The router is not ready. Try again in a moment."}}
	}
	rec := &capture{header: http.Header{}}
	s.handler.ServeHTTP(rec, inProcess(r, http.MethodGet, path, query, nil))
	if rec.status >= http.StatusBadRequest {
		code := rpcInternalError
		if rec.status == http.StatusNotFound || rec.status == http.StatusForbidden {
			code = rpcResourceNotFound
		}
		return &resourceError{rpcError{Code: code, Message: rec.toolResult().Content[0].Text}}
	}
	if err := json.Unmarshal(rec.body.Bytes(), into); err != nil {
		return &resourceError{rpcError{Code: rpcInternalError, Message: "The answer could not be read. Try again in a moment."}}
	}
	return nil
}

// resourceError is a read refused, which names the address it was refused for.
type resourceError struct{ rpcError }

func (e *resourceError) about(uri string) *rpcError {
	out := e.rpcError
	if out.Code == rpcResourceNotFound {
		out.Data = map[string]any{"uri": uri}
	}
	return &out
}
