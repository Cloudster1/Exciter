package stator

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// linkRun is one event's sync, as one person.
type linkRun struct {
	sync          *LinkSync
	tx            db.DBTX
	requirementID uuid.UUID
	// as is the actor's own context, for what the service writes as them.
	as     context.Context
	viewer *Viewer
	status Status
	name   string
}

// apply makes one step in Stator and records it. A retry is a Stator that did not answer: the event fails and the
// outbox tries it again.
func (r *linkRun) apply(ctx context.Context, step LinkStep) (retry error, err error) {
	if step.Want == nil && (step.Have == nil || step.Have.RemoteID == nil) {
		return nil, r.forget(ctx, step.PageID)
	}
	if r.viewer == nil {
		return nil, r.fail(ctx, step, r.withoutToken())
	}
	old := step.Have
	if old != nil && old.RemoteID != nil && (step.Want == nil || old.URL != step.Want.URL) {
		if why, retry := r.call(ctx, http.MethodDelete, linkPath(step.PageID)+"/"+old.RemoteID.String(), nil, nil); retry != nil || why != "" {
			if retry != nil {
				return retry, nil
			}
			return nil, r.fail(ctx, step, why)
		}
		if step.Want == nil {
			return nil, r.forget(ctx, step.PageID)
		}
		if _, err := r.tx.Exec(ctx, `
			UPDATE stator_remote_link SET remote_link_id = NULL, updated_at = now()
			WHERE requirement_id = $1 AND page_id = $2`, r.requirementID, step.PageID); err != nil {
			return nil, err
		}
	}
	var answer struct {
		RemoteLink struct {
			ID uuid.UUID `json:"id"`
		} `json:"remoteLink"`
	}
	icon := strings.TrimSuffix(r.sync.svc.opts.AppURL, "/") + IconPath
	body := RemoteLinkRequest{URL: step.Want.URL, Title: step.Want.Title, Source: LinkSource, IconURL: icon}
	why, retry := r.call(ctx, http.MethodPost, linkPath(step.PageID), body, &answer)
	switch {
	case retry != nil:
		return retry, nil
	case why != "":
		return nil, r.fail(ctx, step, why)
	}
	_, err = r.tx.Exec(ctx, `
		INSERT INTO stator_remote_link (requirement_id, page_id, remote_link_id, url, title, state, error, synced_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'synced', NULL, now(), now())
		ON CONFLICT (requirement_id, page_id) DO UPDATE SET
		    remote_link_id = EXCLUDED.remote_link_id, url = EXCLUDED.url, title = EXCLUDED.title,
		    state = 'synced', error = NULL, synced_at = now(), updated_at = now()`,
		r.requirementID, step.PageID, answer.RemoteLink.ID, step.Want.URL, step.Want.Title)
	return nil, err
}

// call sends one request as the run's person. why is the sentence for a refusal another try would meet again;
// retry is a Stator that did not answer. A 404 on a delete means the link or its page is gone already.
func (r *linkRun) call(ctx context.Context, method, path string, body, out any) (why string, retry error) {
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	err := r.viewer.Caller.Send(callCtx, method, path, body, out)
	var refused *RefusedError
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, ErrRejected):
		if nerr := r.sync.svc.NoteRejected(r.as, r.viewer); nerr != nil {
			r.sync.log.Warn("a rejected Stator token could not be marked", "error", nerr)
		}
		r.viewer, r.status = nil, StatusRejected
		return r.withoutToken(), nil
	case errors.As(err, &refused) && refused.Status == http.StatusNotFound && method == http.MethodDelete:
		return "", nil
	case errors.As(err, &refused):
		return r.refusal(refused), nil
	default:
		r.sync.log.Info("Stator did not answer a link sync; the outbox tries again", "requirement_id", r.requirementID, "error", err)
		return "", err
	}
}

// again ends every sentence of a failed link.
const again = " The next change to the requirement tries again."

func (r *linkRun) refusal(e *RefusedError) string {
	switch {
	case e.Code == "read_only_token":
		return r.person() + "'s Stator token may only read. Store one that may write under Your account, Stator account." + again
	case e.Status == http.StatusForbidden:
		return "Stator did not let " + r.person() + " put requirements on this page. Somebody who may edit the page in Stator can change the requirement." + again
	case e.Status == http.StatusNotFound:
		return "Stator has no page with this id that " + r.person() + " may read. Check the page, or remove the link." + again
	}
	return "Stator refused the link: " + sentence(e.Message) + again
}

// sentence ends Stator's own words with a full stop, as every sentence Exciter shows does.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

func (r *linkRun) withoutToken() string {
	if r.status == StatusRejected {
		return r.person() + "'s Stator token was not accepted. Store a new one under Your account, Stator account." + again
	}
	return r.person() + " has not connected a Stator account, so the link was not put on the page. Connect one under Your account, Stator account." + again
}

// person names the actor in a sentence that begins with it.
func (r *linkRun) person() string {
	if r.name == "" {
		return "The person who changed the requirement"
	}
	return r.name
}

// pending marks every link about to be put as not there yet, which it stays while Stator does not answer.
func (r *linkRun) pending(ctx context.Context, steps []LinkStep) error {
	var pages []uuid.UUID
	for _, step := range steps {
		if step.Want != nil {
			pages = append(pages, step.PageID)
		}
	}
	_, err := r.tx.Exec(ctx, `
		INSERT INTO stator_remote_link (requirement_id, page_id, state, updated_at)
		SELECT $1, p, 'pending', now() FROM unnest($2::uuid[]) AS p
		ON CONFLICT (requirement_id, page_id) DO UPDATE SET state = 'pending', error = NULL, updated_at = now()`,
		r.requirementID, pages)
	return err
}

// fail marks a page failed with why, keeping what was recorded of its link so a later sync can still take it off.
func (r *linkRun) fail(ctx context.Context, step LinkStep, why string) error {
	_, err := r.tx.Exec(ctx, `
		INSERT INTO stator_remote_link (requirement_id, page_id, state, error, updated_at)
		VALUES ($1, $2, 'failed', $3, now())
		ON CONFLICT (requirement_id, page_id) DO UPDATE SET state = 'failed', error = EXCLUDED.error, updated_at = now()`,
		r.requirementID, step.PageID, why)
	return err
}

func (r *linkRun) forget(ctx context.Context, page uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM stator_remote_link WHERE requirement_id = $1 AND page_id = $2`, r.requirementID, page)
	return err
}

func linkPath(page uuid.UUID) string {
	return "/pages/" + page.String() + "/remote-links"
}
