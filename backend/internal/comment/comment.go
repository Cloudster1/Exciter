// Package comment keeps the threaded discussion on a requirement (#33); docs/api-contract-m3.md says how, and the
// shapes here are the agreed ones, Stator's page comments over requirements.
package comment

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// Agreed limits, Stator's where Stator has them.
const (
	// MaxBodyBytes bounds one comment's document; the body is the requirement editor's comment subset.
	MaxBodyBytes = document.MaxCommentBytes
	// MentionableDefaultLimit and MentionableMaxLimit bound the mention picker's answer.
	MentionableDefaultLimit = 20
	MentionableMaxLimit     = 50
)

// Thread is a requirement's discussion that began with one comment: its id is that comment's, and every reply follows
// it in order, one level deep.
type Thread struct {
	ID            uuid.UUID  `json:"id"`
	RequirementID uuid.UUID  `json:"requirementId"`
	Resolved      bool       `json:"resolved"`
	ResolvedBy    string     `json:"resolvedByName"`
	ResolvedAt    *time.Time `json:"resolvedAt"`
	Comments      []Comment  `json:"comments"`
	Can           ThreadCan  `json:"can"`
}

// ThreadCan is what the caller may do to a thread.
type ThreadCan struct {
	Reply   bool `json:"reply"`
	Resolve bool `json:"resolve"`
}

// Comment is one comment; a deleted one keeps its place and author with no body.
type Comment struct {
	ID         uuid.UUID  `json:"id"`
	ThreadID   uuid.UUID  `json:"threadId"`
	AuthorID   *uuid.UUID `json:"authorId"`
	AuthorName string     `json:"authorName"`
	// Revision is the requirement's revision when the comment was written, so a reader sees what it was about.
	Revision int `json:"revision"`
	// Body is the comment's rich text document, null once deleted.
	Body any `json:"body"`
	// BodyMarkdown is the body as Markdown, answered when a read asks for it and where Markdown holds it (#186).
	BodyMarkdown *string    `json:"bodyMarkdown,omitempty"`
	Deleted      bool       `json:"deleted"`
	CreatedAt    time.Time  `json:"createdAt"`
	EditedAt     *time.Time `json:"editedAt"`
	// Via is the token or connected app the comment was written through, null for a session (#190).
	Via *via.Via `json:"via"`
	Can Can      `json:"can"`
}

// Can is what the caller may do to one comment.
type Can struct {
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
}

// Input is a comment's body, a document in the comment subset of the requirement editor.
type Input struct {
	Body any `json:"body,omitempty"`
	// BodyMarkdown is the same written as Markdown, either form but not both (#186).
	BodyMarkdown *string `json:"bodyMarkdown,omitempty"`
}

// Mentionable is somebody the mention picker offers, and whether they may read the requirement.
type Mentionable struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	CanView bool      `json:"canView"`
}
