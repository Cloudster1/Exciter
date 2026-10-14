package notify

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/mail"
)

// PreferencesPath is where the web client lets a person choose what they hear.
const PreferencesPath = "/settings/notifications"

// someone names an actor whose account is gone.
const someone = "Somebody"

// mailed is one notification as a mail words it. Mail is the one place the server writes sentences: the client
// words the rows itself.
type mailed struct {
	kind          Kind
	actor         string
	requirementID uuid.UUID
	key           string
	title         string
	subject       Subject
}

// Sentence says what happened, in English.
func Sentence(kind Kind, actor, key, title string) string {
	if actor == "" {
		actor = someone
	}
	named := fmt.Sprintf(`%s "%s"`, key, title)
	switch kind {
	case KindMentioned:
		return fmt.Sprintf("%s mentioned you in a comment on %s", actor, named)
	case KindReviewRequested:
		return fmt.Sprintf("%s asks you to review %s", actor, named)
	case KindReviewed:
		return fmt.Sprintf("The review of %s is decided", named)
	case KindSuspect:
		return fmt.Sprintf("A link of %s needs checking after %s revised the other end", named, actor)
	case KindReplied:
		return fmt.Sprintf("%s replied in a thread on %s", actor, named)
	case KindCommented:
		return fmt.Sprintf("%s commented on %s", actor, named)
	case KindResolved:
		return fmt.Sprintf("%s resolved or reopened a thread on %s", actor, named)
	case KindRevised:
		return fmt.Sprintf("%s saved a new revision of %s", actor, named)
	case KindTransitioned:
		return fmt.Sprintf("%s moved %s to another status", actor, named)
	}
	return fmt.Sprintf("%s changed %s", actor, named)
}

// RequirementURL is where a notification leads: the requirement's permalink, at its thread when there is one.
func RequirementURL(appURL string, requirementID uuid.UUID, s Subject) string {
	link := fmt.Sprintf("%s/r/%s", appURL, requirementID)
	if s.ThreadID != nil {
		link += ThreadAnchor(*s.ThreadID)
	}
	return link
}

// ThreadAnchor is the fragment that opens a requirement at one of its threads.
func ThreadAnchor(id uuid.UUID) string { return "#thread-" + id.String() }

func (m mailed) sentence() string { return Sentence(m.kind, m.actor, m.key, m.title) }

// single is the mail for one notification sent at once.
func (m mailed) single(to, appURL string) mail.Mail {
	var b strings.Builder
	b.WriteString(m.sentence() + ".\n\n")
	if m.subject.Excerpt != "" {
		b.WriteString(m.subject.Excerpt + "\n\n")
	}
	fmt.Fprintf(&b, "Open the requirement: %s\n\n", RequirementURL(appURL, m.requirementID, m.subject))
	fmt.Fprintf(&b, "Choose what you are told about: %s%s\n", appURL, PreferencesPath)
	return mail.Mail{To: to, Subject: m.sentence(), Body: b.String()}
}

// bundle is the mail for everything a digest gathered.
func bundle(to, appURL string, items []mailed) mail.Mail {
	var b strings.Builder
	noun := "updates"
	if len(items) == 1 {
		noun = "update"
	}
	fmt.Fprintf(&b, "%d %s since your last mail:\n\n", len(items), noun)
	for _, m := range items {
		fmt.Fprintf(&b, "- %s\n  %s\n", m.sentence(), RequirementURL(appURL, m.requirementID, m.subject))
		if m.subject.Excerpt != "" {
			fmt.Fprintf(&b, "  %s\n", m.subject.Excerpt)
		}
	}
	fmt.Fprintf(&b, "\nChoose what you are told about: %s%s\n", appURL, PreferencesPath)
	return mail.Mail{To: to, Subject: fmt.Sprintf("%d %s in Exciter", len(items), noun), Body: b.String()}
}
