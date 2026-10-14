package document

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestACommentHoldsTextAndMentionsAlone(t *testing.T) {
	ada := uuid.New()
	ok := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Ask ","marks":[{"type":"bold"}]},
		{"type":"mention","attrs":{"id":"` + ada.String() + `","label":"Ada"}}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"why"}]}]}]}]}`
	if _, err := ParseComment(json.RawMessage(ok)); err != nil {
		t.Fatalf("a comment of text, a list and a mention is refused: %v", err)
	}
	for name, body := range map[string]string{
		"a table":       `{"type":"doc","content":[{"type":"table","content":[]}]}`,
		"an image":      `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"` + uuid.NewString() + `"}}]}`,
		"a rule":        `{"type":"doc","content":[{"type":"horizontalRule"}]}`,
		"no document":   `{"type":"paragraph"}`,
		"too long text": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + strings.Repeat("x", MaxCommentBytes) + `"}]}]}`,
	} {
		if _, err := ParseComment(json.RawMessage(body)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
	if _, ok := CommentAllowed.Nodes["table"]; ok {
		t.Error("the comment allowlist keeps tables")
	}
	for _, child := range CommentAllowed.Nodes["doc"].Content {
		if _, kept := CommentAllowed.Nodes[child]; !kept {
			t.Errorf("a comment's document may hold %q, which the subset dropped", child)
		}
	}
}

func TestMentionsAreReadOnceAndAnEditTellsOnlyTheNewOnes(t *testing.T) {
	ada, bob := uuid.New(), uuid.New()
	mention := func(id uuid.UUID, label string) string {
		return `{"type":"mention","attrs":{"id":"` + id.String() + `","label":"` + label + `"}}`
	}
	before := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[` + mention(ada, "Ada") + `,{"type":"text","text":" and "},` + mention(ada, "Ada") + `]}]}`)
	after := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[` + mention(ada, "Ada") + `,{"type":"text","text":" asks "},` + mention(bob, "Bob") + `]}]}`)
	if got := MentionIDs(MentionsIn(before)); len(got) != 1 || got[0] != ada {
		t.Errorf("before names %v, want Ada once", got)
	}
	if got := MentionIDs(NewMentions(MentionsIn(before), MentionsIn(after))); len(got) != 1 || got[0] != bob {
		t.Errorf("the edit newly names %v, want Bob alone", got)
	}
	if got := MentionsIn(json.RawMessage(`{"type":"table"}`)); got != nil {
		t.Errorf("a body that does not parse names %v", got)
	}
	if got := MentionIDs(nil); got == nil || len(got) != 0 {
		t.Error("no mentions is not an empty list")
	}
}
