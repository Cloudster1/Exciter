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
	// session is the review session it is about instead of a requirement (#204); key is then its project's.
	session *uuid.UUID
	// scan is the code scan it is about (#201); key is then its project's and title its repository and branch.
	scan *uuid.UUID
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
		return fmt.Sprintf("%s decided the review of %s", actor, named)
	case KindSuspect:
		return fmt.Sprintf("A link of %s needs checking after %s revised the other end", named, actor)
	case KindSourceRevised:
		return fmt.Sprintf("The library source of %s has a new revision from %s", named, actor)
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
	case KindSessionInvited:
		return fmt.Sprintf(`%s asks you to take part in the review session "%s" of %s`, actor, title, key)
	case KindSessionRound:
		return fmt.Sprintf(`%s started a new round of the review session "%s" of %s`, actor, title, key)
	case KindSessionReminder:
		return fmt.Sprintf(`The review session "%s" of %s ends soon, and waits for you`, title, key)
	case KindSessionClosed:
		return fmt.Sprintf(`%s closed the review session "%s" of %s`, actor, title, key)
	case KindCodeOutdated:
		return fmt.Sprintf("A scan of %s in %s brought code tags naming older revisions of requirements you follow", title, key)
	case KindTermChanged:
		return fmt.Sprintf("A term %s uses has a new definition from %s", named, actor)
	case KindCodeOrphaned:
		return fmt.Sprintf("A scan of %s in %s brought code tags naming requirements in the trash or none", title, key)
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

// SessionURL is where a notification about a review session leads: the session's page.
func SessionURL(appURL, projectKey string, id uuid.UUID) string {
	return fmt.Sprintf("%s/p/%s/sessions/%s", appURL, projectKey, id)
}

// CodeTagsURL is where a notification about a code scan leads: the project's outdated and orphaned tags.
func CodeTagsURL(appURL, projectKey string) string {
	return fmt.Sprintf("%s/p/%s/code-tags", appURL, projectKey)
}

// link is where one mailed notification leads.
func (m mailed) link(appURL string) string {
	if m.scan != nil {
		return CodeTagsURL(appURL, m.key)
	}
	if m.session != nil {
		return SessionURL(appURL, m.key, *m.session)
	}
	return RequirementURL(appURL, m.requirementID, m.subject)
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
	switch {
	case m.scan != nil:
		fmt.Fprintf(&b, "Open the code tags: %s\n\n", m.link(appURL))
	case m.session != nil:
		fmt.Fprintf(&b, "Open the session: %s\n\n", m.link(appURL))
	default:
		fmt.Fprintf(&b, "Open the requirement: %s\n\n", m.link(appURL))
	}
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
		fmt.Fprintf(&b, "- %s\n  %s\n", m.sentence(), m.link(appURL))
		if m.subject.Excerpt != "" {
			fmt.Fprintf(&b, "  %s\n", m.subject.Excerpt)
		}
	}
	fmt.Fprintf(&b, "\nChoose what you are told about: %s%s\n", appURL, PreferencesPath)
	return mail.Mail{To: to, Subject: fmt.Sprintf("%d %s in Exciter", len(items), noun), Body: b.String()}
}
