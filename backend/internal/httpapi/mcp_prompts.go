package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// The prompts an assistant is started on (#187), English and kept beside the tools they name, so a renamed tool
// is found here by the test that holds each name to the catalog.

type mcpPrompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Arguments   []promptArgument `json:"arguments"`
	// messages writes the prompt with its arguments in place, reading what it embeds as the caller.
	messages func(s *Server, r *http.Request, args map[string]string) ([]promptMessage, *rpcError)
	// tools are the tools the prompt names, held to the catalog by a test.
	tools []string
}

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type promptMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func userText(text string) promptMessage {
	return promptMessage{Role: "user", Content: map[string]any{"type": "text", "text": text}}
}

var (
	argProject = promptArgument{Name: "projectKey", Description: "The key of the project, such as CHK.", Required: true}
	argSection = promptArgument{Name: "section", Description: "The key of a section or requirement; the whole specification when absent."}
)

// reviewBases are the bases review_section compares with, the first when none is given.
var reviewBases = []string{string(requirement.BaseApproved), string(requirement.BasePrevious)}

var prompts = []mcpPrompt{
	{
		Name: "review_section", Title: "Review a section's changes",
		Description: "Read what changed in a section since its approval, or since each requirement's previous revision, and comment on each finding.",
		Arguments: []promptArgument{argProject, argSection, {Name: "base",
			Description: "What to compare with: approved, each requirement's last approval, or previous, its previous revision; approved when absent."}},
		tools:    []string{"get_spec", "diff_revisions", "comment_on_requirement"},
		messages: reviewSection,
	},
	{
		Name: "write_requirements", Title: "Write requirements from a text",
		Description: "Draft requirements in a project, under a section when one is given, from a source text such as notes or a specification.",
		Arguments: []promptArgument{argProject, argSection,
			{Name: "source", Description: "The text to draft the requirements from.", Required: true}},
		tools:    []string{"list_types", "list_attributes", "get_spec", "create_requirement"},
		messages: writeRequirements,
	},
	{
		Name: "check_quality", Title: "Check a requirement's quality",
		Description: "Hold one requirement to the characteristics of a well-formed requirement, and say how it could read better.",
		Arguments:   []promptArgument{{Name: "reqKey", Description: "The key of the requirement, such as CHK-R42.", Required: true}},
		tools:       []string{"get_spec", "get_requirement", "comment_on_requirement"},
		messages:    checkQuality,
	},
}

func promptList() []mcpPrompt { return prompts }

func (s *Server) getPrompt(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "A prompt is asked for by its name, with its arguments as an object of strings."}
	}
	i := slices.IndexFunc(prompts, func(each mcpPrompt) bool { return each.Name == p.Name })
	if i < 0 {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "There is no prompt " + p.Name + ". Ask prompts/list for the names."}
	}
	prompt := prompts[i]
	args := map[string]string{}
	for name, value := range p.Arguments {
		if !slices.ContainsFunc(prompt.Arguments, func(a promptArgument) bool { return a.Name == name }) {
			return nil, &rpcError{Code: rpcInvalidParams, Message: fmt.Sprintf("The prompt %s does not take %s.", prompt.Name, name)}
		}
		if value = strings.TrimSpace(value); value != "" {
			args[name] = value
		}
	}
	for _, a := range prompt.Arguments {
		if _, given := args[a.Name]; a.Required && !given {
			return nil, &rpcError{Code: rpcInvalidParams, Message: fmt.Sprintf("The prompt %s needs %s: %s", prompt.Name, a.Name, lowerFirst(a.Description))}
		}
	}
	messages, err := prompt.messages(s, r, args)
	if err != nil {
		return nil, err
	}
	return map[string]any{"description": prompt.Description, "messages": messages}, nil
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// noDecision ends each prompt, as an assistant asks for reviews and never decides one.
const noDecision = "You do not approve or reject anything: a person decides each review in Exciter."

func reviewSection(_ *Server, _ *http.Request, args map[string]string) ([]promptMessage, *rpcError) {
	base := args["base"]
	if base == "" {
		base = reviewBases[0]
	}
	if !slices.Contains(reviewBases, base) {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "The base is approved or previous."}
	}
	scope, keep := "the specification of project "+args["projectKey"], ""
	if section := args["section"]; section != "" {
		scope = "section " + section + " of project " + args["projectKey"]
		keep = " Keep to " + section + " and the entries whose outline begins with its outline."
	}
	since := map[string]string{string(requirement.BaseApproved): "its last approval", string(requirement.BasePrevious): "each requirement's previous revision"}[base]
	return []promptMessage{userText(fmt.Sprintf(`Review what changed in %s since %s.

1. Call get_spec with projectKey %s, changes %s and onlyChanged true, a window at a time with limit and offset until every entry is read.%s
2. For each requirement that changed, read what its change says it differs in, and diff_revisions where you need the words before and after. Judge whether the change is clear, complete, verifiable and consistent with the requirements around it.
3. For each finding, start a thread on that requirement with comment_on_requirement: what you found and what you suggest, in Markdown in bodyMarkdown. One thread for each finding; a sound change needs none.

Then sum up what you found, by requirement key. %s`, scope, since, args["projectKey"], base, keep, noDecision))}, nil
}

func writeRequirements(_ *Server, _ *http.Request, args map[string]string) ([]promptMessage, *rpcError) {
	where, read, place := "", "the specification", ""
	if section := args["section"]; section != "" {
		where = " under " + section
		read = section + " and what is below it, and note the id of " + section
		place = " Place it under " + section + " with place.parentId set to that id."
	}
	return []promptMessage{userText(fmt.Sprintf(`Draft requirements in project %s%s from the source text below.

1. Call list_types with projectKey %s to choose the type of each requirement, and list_attributes for the values a type requires.
2. Call get_spec with projectKey %s and read %s, so that nothing already written is written twice.
3. For each requirement the text asks for, call create_requirement with projectKey %s, a short title, its typeId, and its statement in Markdown in statementMarkdown: one requirement in each statement, singular and verifiable, written with shall.%s Give why it is needed in rationaleMarkdown when the text says.

Write nothing the text does not ask for, and ask before writing what it leaves unclear. Then list what you wrote, by key.

<source>
%s
</source>`, args["projectKey"], where, args["projectKey"], args["projectKey"], read, args["projectKey"], place, args["source"]))}, nil
}

func checkQuality(s *Server, r *http.Request, args map[string]string) ([]promptMessage, *rpcError) {
	uri := "exciter://requirements/" + args["reqKey"]
	text, err := s.resourceText(r, uri)
	if err != nil {
		return nil, err
	}
	return []promptMessage{
		{Role: "user", Content: map[string]any{"type": "resource", "resource": map[string]any{"uri": uri, "mimeType": markdownMIME, "text": text}}},
		userText(fmt.Sprintf(`Hold requirement %s, above, to the characteristics of a well-formed requirement:

- necessary: something would be missing without it
- singular: it states one thing
- unambiguous: it reads one way only
- verifiable: a test, inspection, analysis or demonstration can show that it is met
- feasible: it can be met with what is known of the system
- free of design: it says what is needed, not how to build it
- consistent: it agrees with the requirements around it and the terms they use; read them with get_spec or get_requirement where it depends on them

For each characteristic, say whether it holds; where it does not, say why and give a statement that would. Change nothing. If asked to keep the findings, start a thread on the requirement with comment_on_requirement. %s`, args["reqKey"], noDecision)),
	}, nil
}
