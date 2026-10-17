//go:build integration

package test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The document as Markdown holds what the Word file holds, in the Markdown of #186, and leaving is recorded (#187).
func TestADocumentDownloadsAsMarkdown(t *testing.T) {
	s := newDocumentScene(t)
	got := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document.md"), http.StatusOK, "a member exports Markdown")
	if got.Header.Get("Content-Type") != "text/markdown; charset=utf-8" || !strings.Contains(got.Header.Get("Content-Disposition"), s.key+"-") ||
		got.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("the download: %v", got.Header)
	}
	body := string(got.Raw)
	for _, want := range []string{"# Brakes\n", "Specification of Brakes (" + s.key + ")", "## 1 " + s.key + "-R1 Braking",
		"### 1.1 " + s.key + "-R2 Stop within 40 m", "- Priority: must", "- Safe: yes", "- Detail: Measured on asphalt.", "**Rationale**\n\nThe law says so.",
		"exciter:attachment/", "Warn the driver loudly", "**Links**", s.key + "-R3 Warn the driver loudly"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Markdown lacks %q:\n%s", want, body)
		}
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'specification.exported' AND data->>'project' = $1 AND data->>'format' = 'md'
		AND actor_user_id = $2`, s.key, s.f.plain.user); n != 1 {
		t.Errorf("%d audit entries for the member's export, want 1", n)
	}
	baseline := want(t, s.f.owner.get(t, "/api/v1/baselines/"+s.baseline+"/document.md"), http.StatusOK, "export the baseline as Markdown")
	if text := string(baseline.Raw); !strings.Contains(text, "Baseline of Brakes") || !strings.Contains(text, "Warn the driver\n") {
		t.Errorf("the baseline's Markdown:\n%s", text)
	}
	refuse(t, "a section the project lacks", s.f.owner.get(t, "/api/v1/projects/"+s.key+"/document.md?section="+s.key+"-R99"), http.StatusNotFound, "not_found")
	refuse(t, "somebody who does not view the project", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/document.md"), http.StatusNotFound, "not_found")
}

// An assistant held to one project reads its specification and its requirements whole, finds nothing of another, and
// is started on common work by prompts that read as the caller too.
func TestAnAssistantReadsResourcesAndIsStartedByPrompts(t *testing.T) {
	s := newDocumentScene(t)
	other := projectKey(t, "ELS")
	want(t, s.f.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Elsewhere"}), http.StatusCreated, "make "+other)
	want(t, s.f.owner.post(t, "/api/v1/projects/"+other+"/requirements", map[string]any{"title": "Hidden"}), http.StatusCreated, "write "+other+"-R1")
	_, secret := makeToken(t, s.f.owner, map[string]any{"name": "one project", "projects": []string{s.key}})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)

	t.Run("it lists the specifications of what it may read, and the templates", func(t *testing.T) {
		var uris []string
		for r, err := range a.session.Resources(a.ctx, nil) {
			if err != nil {
				t.Fatal(err)
			}
			uris = append(uris, r.URI)
		}
		if !slices.Contains(uris, "exciter://openapi.json") || !slices.Contains(uris, "exciter://projects/"+s.key+"/spec") ||
			slices.Contains(uris, "exciter://projects/"+other+"/spec") {
			t.Errorf("the resources are %v", uris)
		}
		templates, err := a.session.ListResourceTemplates(a.ctx, nil)
		if err != nil || len(templates.ResourceTemplates) != 3 {
			t.Fatalf("the templates: %+v, %v", templates, err)
		}
	})

	t.Run("it reads a specification, a section and a requirement as Markdown", func(t *testing.T) {
		read := func(uri string) string {
			got, err := a.session.ReadResource(a.ctx, &mcp.ReadResourceParams{URI: uri})
			if err != nil {
				t.Fatalf("%s: %v", uri, err)
			}
			if got.Contents[0].MIMEType != "text/markdown" || got.Contents[0].URI != uri {
				t.Errorf("%s answers %+v", uri, got.Contents[0])
			}
			return got.Contents[0].Text
		}
		if whole := read("exciter://projects/" + s.key + "/spec"); !strings.Contains(whole, "### 1.1 "+s.key+"-R2 Stop within 40 m") ||
			!strings.Contains(whole, "## 2 "+s.key+"-R3") {
			t.Errorf("the specification reads:\n%s", whole)
		}
		section := read("exciter://projects/" + s.key + "/spec?section=" + s.key + "-R1")
		if !strings.Contains(section, "Section "+s.key+"-R1 of Brakes") || !strings.Contains(section, s.key+"-R2") || strings.Contains(section, "## 2 ") {
			t.Errorf("the section reads:\n%s", section)
		}
		if one := read("exciter://requirements/" + s.key + "-R2"); !strings.Contains(one, "## 1.1 "+s.key+"-R2 Stop within 40 m") ||
			strings.Contains(one, "Braking\n") {
			t.Errorf("the requirement reads:\n%s", one)
		}
		baseline := read("exciter://baselines/" + s.baseline + "/spec")
		if !strings.Contains(baseline, "Warn the driver\n") {
			t.Errorf("the baseline reads:\n%s", baseline)
		}
	})

	t.Run("what lies outside its project is not found", func(t *testing.T) {
		for _, uri := range []string{"exciter://projects/" + other + "/spec", "exciter://requirements/" + other + "-R1", "exciter://projects/" + s.key + "/notes"} {
			_, err := a.session.ReadResource(a.ctx, &mcp.ReadResourceParams{URI: uri})
			var refused *jsonrpc.Error
			if !errors.As(err, &refused) || refused.Code != jsonrpc.CodeInvalidParams || refused.Message == "" {
				t.Errorf("%s answers %v", uri, err)
			}
		}
	})

	t.Run("a prompt is written with its arguments, and embeds what it reads as the caller", func(t *testing.T) {
		listed, err := a.session.ListPrompts(a.ctx, nil)
		if err != nil || len(listed.Prompts) != 3 {
			t.Fatalf("the prompts: %+v, %v", listed, err)
		}
		got, err := a.session.GetPrompt(a.ctx, &mcp.GetPromptParams{Name: "check_quality", Arguments: map[string]string{"reqKey": s.key + "-R2"}})
		if err != nil || len(got.Messages) != 2 {
			t.Fatalf("check_quality: %+v, %v", got, err)
		}
		embedded, ok := got.Messages[0].Content.(*mcp.EmbeddedResource)
		if !ok || !strings.Contains(embedded.Resource.Text, "Stop within 40 m") {
			t.Errorf("check_quality embeds %+v", got.Messages[0].Content)
		}
		if text := got.Messages[1].Content.(*mcp.TextContent).Text; !strings.Contains(text, "requirement "+s.key+"-R2") || !strings.Contains(text, "verifiable") {
			t.Errorf("check_quality asks %q", text)
		}
		review, err := a.session.GetPrompt(a.ctx, &mcp.GetPromptParams{Name: "review_section", Arguments: map[string]string{"projectKey": s.key, "base": "previous"}})
		if err != nil || !strings.Contains(review.Messages[0].Content.(*mcp.TextContent).Text, "changes previous") {
			t.Errorf("review_section: %+v, %v", review, err)
		}
		if _, err := a.session.GetPrompt(a.ctx, &mcp.GetPromptParams{Name: "check_quality", Arguments: map[string]string{"reqKey": other + "-R1"}}); err == nil {
			t.Error("check_quality read a requirement outside the token's project")
		}
		if _, err := a.session.GetPrompt(a.ctx, &mcp.GetPromptParams{Name: "write_requirements", Arguments: map[string]string{"projectKey": s.key}}); err == nil ||
			!strings.Contains(err.Error(), "needs source") {
			t.Errorf("a prompt without its source: %v", err)
		}
	})
}
