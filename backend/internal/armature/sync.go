package armature

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// Link sync (#27), Stator's: the worker brings the remote links of one requirement in Armature in line with its
// links, as the person whose change it was.

const (
	// TopicLinks names a requirement whose remote links to sync; the database writes it.
	TopicLinks = "armature.links"
	// RequirementPath and IconPath are where a link on an issue leads, and its icon, under Exciter's origin.
	RequirementPath = "/r/"
	IconPath        = "/favicon-32.png"
)

// LinksEvent is what an armature.links event carries.
type LinksEvent struct {
	RequirementID uuid.UUID `json:"requirementId"`
	ActorID       uuid.UUID `json:"actorId"`
}

// LinkTitle is how a requirement is titled on an issue: its key, its title and how it relates, cut to Armature's
// bound.
func LinkTitle(reqKey, title string, relation Relation) string {
	full := reqKey + " " + strings.TrimSpace(title) + " (" + string(relation) + ")"
	if utf8.RuneCountInString(full) <= MaxLinkTitleLength {
		return full
	}
	// The relation stays readable: the title gives way, ending in an ellipsis of three dots.
	tail := "... (" + string(relation) + ")"
	head := []rune(reqKey + " " + strings.TrimSpace(title))
	return string(head[:MaxLinkTitleLength-utf8.RuneCountInString(tail)]) + tail
}

// WantedLink is the remote link a requirement should have on one issue.
type WantedLink struct {
	URL   string
	Title string
}

// LinkRecord is what the last sync did for one issue.
type LinkRecord struct {
	Key      string
	RemoteID *uuid.UUID
	URL      string
	Title    string
	State    SyncState
}

// LinkStep is one change to make in Armature: Want set puts the link, nil takes Have's off the issue.
type LinkStep struct {
	Key  string
	Want *WantedLink
	Have *LinkRecord
}

// PlanLinks compares what a requirement should have in Armature with what the last sync recorded, by key. A link
// synced as wanted is left alone; a failed or pending one is tried again.
func PlanLinks(want map[string]WantedLink, have []LinkRecord) []LinkStep {
	held := make(map[string]*LinkRecord, len(have))
	for i := range have {
		held[have[i].Key] = &have[i]
	}
	var steps []LinkStep
	for key, w := range want {
		rec := held[key]
		if rec != nil && rec.State == SyncSynced && rec.RemoteID != nil && rec.URL == w.URL && rec.Title == w.Title {
			continue
		}
		steps = append(steps, LinkStep{Key: key, Want: &WantedLink{URL: w.URL, Title: w.Title}, Have: rec})
	}
	for key, rec := range held {
		if _, ok := want[key]; !ok {
			steps = append(steps, LinkStep{Key: key, Have: rec})
		}
	}
	slices.SortFunc(steps, func(a, b LinkStep) int { return strings.Compare(a.Key, b.Key) })
	return steps
}

// WantedLinks are the remote links a live requirement should have: one per issue it is traced to, titled with
// the first relation in Relations' order. Broken and detached links want none, and a trashed requirement none.
func WantedLinks(appURL string, req RequirementState, links []LinkedIssue) map[string]WantedLink {
	want := map[string]WantedLink{}
	if !req.Found || req.Trashed {
		return want
	}
	first := map[string]Relation{}
	for _, l := range links {
		if l.Broken || l.Detached {
			continue
		}
		if had, ok := first[l.Key]; !ok || slices.Index(Relations, l.Relation) < slices.Index(Relations, had) {
			first[l.Key] = l.Relation
		}
	}
	target := strings.TrimSuffix(appURL, "/") + RequirementPath + req.ID.String()
	for key, relation := range first {
		want[key] = WantedLink{URL: target, Title: LinkTitle(req.Key, req.Title, relation)}
	}
	return want
}

// RequirementState is what a sync needs of a requirement; Found is false once it is purged.
type RequirementState struct {
	ID      uuid.UUID
	Found   bool
	Key     string
	Title   string
	Trashed bool
}

// LinkedIssue is one of a requirement's links as the sync sees it.
type LinkedIssue struct {
	Key      string
	Relation Relation
	Broken   bool
	Detached bool
}

// RemoteLinkRequest is what link sync sends Armature to put a requirement on an issue.
type RemoteLinkRequest struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Source  string `json:"source"`
	IconURL string `json:"iconUrl"`
}

// LinkSync is the worker's handler for armature.links.
type LinkSync struct {
	svc *Service
	log *slog.Logger
}

// NewLinkSync builds the handler on the service whose AppURL the links name.
func NewLinkSync(svc *Service, log *slog.Logger) *LinkSync {
	return &LinkSync{svc: svc, log: log}
}

// Handle syncs the requirement the event names, as the event's actor with their own token. Run twice, the second
// run finds every link recorded and sends nothing.
func (l *LinkSync) Handle(ctx context.Context, e events.Event) error {
	var in LinksEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil || in.RequirementID == uuid.Nil {
		return nil
	}
	pinned := db.PinPrimary(ctx)
	var retry error
	_, err := l.svc.db.WriteAdmin(pinned, func(ctx context.Context, tx db.DBTX) error {
		// Two workers syncing one requirement at once would each put the links the other is about to record.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('armature-links:' || $1::text, 0))`, in.RequirementID); err != nil {
			return err
		}
		connected, req, links, have, err := l.read(ctx, tx, in.RequirementID)
		if err != nil || !connected {
			return err
		}
		steps := PlanLinks(WantedLinks(l.svc.opts.AppURL, req, links), have)
		if len(steps) == 0 {
			return nil
		}
		run := &linkRun{sync: l, tx: tx, requirementID: in.RequirementID, as: db.WithUser(pinned, in.ActorID)}
		if run.viewer, run.status, err = l.svc.Viewer(run.as); err != nil {
			return err
		}
		if err := run.pending(ctx, steps); err != nil {
			return err
		}
		if run.name, err = actorName(ctx, tx, in.ActorID); err != nil {
			return err
		}
		for _, step := range steps {
			if retry, err = run.apply(ctx, step); err != nil || retry != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return retry
}

func (l *LinkSync) read(ctx context.Context, tx db.DBTX, id uuid.UUID) (bool, RequirementState, []LinkedIssue, []LinkRecord, error) {
	req := RequirementState{ID: id}
	var connected bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM armature_connection)`).Scan(&connected); err != nil || !connected {
		return false, req, nil, nil, err
	}
	err := tx.QueryRow(ctx, `
		SELECT p.key || '-R' || r.number, r.title, r.trashed_at IS NOT NULL
		FROM requirement r JOIN project p ON p.id = r.project_id
		WHERE r.id = $1`, id).Scan(&req.Key, &req.Title, &req.Trashed)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, req, nil, nil, fmt.Errorf("read the requirement: %w", err)
	default:
		req.Found = true
	}
	rows, err := tx.Query(ctx, `
		SELECT issue_key, relation, broken_at IS NOT NULL, detached_at IS NOT NULL
		FROM armature_issue_link WHERE requirement_id = $1`, id)
	if err != nil {
		return false, req, nil, nil, err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LinkedIssue])
	if err != nil {
		return false, req, nil, nil, err
	}
	rows, err = tx.Query(ctx, `
		SELECT issue_key, remote_link_id, COALESCE(url, ''), COALESCE(title, ''), state
		FROM armature_remote_link WHERE requirement_id = $1`, id)
	if err != nil {
		return false, req, nil, nil, err
	}
	have, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LinkRecord])
	return true, req, links, have, err
}

func actorName(ctx context.Context, tx db.DBTX, id uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT NULLIF(name, '') FROM app_user WHERE id = $1), '')`, id).Scan(&name)
	return name, err
}

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

// apply makes one step in Armature and records it. A retry is an Armature that did not answer: the event fails
// and the outbox tries it again.
func (r *linkRun) apply(ctx context.Context, step LinkStep) (retry error, err error) {
	if step.Want == nil && (step.Have == nil || step.Have.RemoteID == nil) {
		return nil, r.forget(ctx, step.Key)
	}
	if r.viewer == nil {
		return nil, r.fail(ctx, step, r.withoutToken())
	}
	if lacks := r.viewer.Offered.Lacks(CapabilityRemoteLinks); lacks != nil {
		return nil, r.fail(ctx, step, lacks.Message+again)
	}
	old := step.Have
	if old != nil && old.RemoteID != nil && (step.Want == nil || old.URL != step.Want.URL) {
		if why, retry := r.call(ctx, http.MethodDelete, linkPath(step.Key)+"/"+old.RemoteID.String(), nil, nil); retry != nil || why != "" {
			if retry != nil {
				return retry, nil
			}
			return nil, r.fail(ctx, step, why)
		}
		if step.Want == nil {
			return nil, r.forget(ctx, step.Key)
		}
		if _, err := r.tx.Exec(ctx, `
			UPDATE armature_remote_link SET remote_link_id = NULL, updated_at = now()
			WHERE requirement_id = $1 AND issue_key = $2`, r.requirementID, step.Key); err != nil {
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
	why, retry := r.call(ctx, http.MethodPost, linkPath(step.Key), body, &answer)
	switch {
	case retry != nil:
		return retry, nil
	case why != "":
		return nil, r.fail(ctx, step, why)
	}
	_, err = r.tx.Exec(ctx, `
		INSERT INTO armature_remote_link (requirement_id, issue_key, remote_link_id, url, title, state, error, synced_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'synced', NULL, now(), now())
		ON CONFLICT (requirement_id, issue_key) DO UPDATE SET
		    remote_link_id = EXCLUDED.remote_link_id, url = EXCLUDED.url, title = EXCLUDED.title,
		    state = 'synced', error = NULL, synced_at = now(), updated_at = now()`,
		r.requirementID, step.Key, answer.RemoteLink.ID, step.Want.URL, step.Want.Title)
	return nil, err
}

// call sends one request as the run's person. why is the sentence for a refusal another try would meet again;
// retry is an Armature that did not answer. A 404 on a delete means the link is gone already.
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
			r.sync.log.Warn("a rejected Armature token could not be marked", "error", nerr)
		}
		r.viewer, r.status = nil, StatusRejected
		return r.withoutToken(), nil
	case errors.As(err, &refused) && refused.Status == http.StatusNotFound && method == http.MethodDelete:
		return "", nil
	case errors.As(err, &refused):
		return r.refusal(refused), nil
	default:
		r.sync.log.Info("Armature did not answer a link sync; the outbox tries again", "requirement_id", r.requirementID, "error", err)
		return "", err
	}
}

// again ends every sentence of a failed link.
const again = " The next change to the requirement tries again."

func (r *linkRun) refusal(e *RefusedError) string {
	switch {
	case e.Code == "read_only_token":
		return r.person() + "'s Armature token may only read. Store one that may write under Your account, Armature account." + again
	case e.Status == http.StatusForbidden:
		return "Armature did not let " + r.person() + " link requirements to this issue. Somebody who may edit the issue in Armature can change the requirement." + again
	case e.Status == http.StatusNotFound:
		return "Armature has no issue with this key that " + r.person() + " may see. Check the key, or remove the link." + again
	}
	return "Armature refused the link: " + strings.TrimSpace(e.Message) + again
}

func (r *linkRun) withoutToken() string {
	if r.status == StatusRejected {
		return r.person() + "'s Armature token was not accepted. Store a new one under Your account, Armature account." + again
	}
	return r.person() + " has not connected an Armature account, so the link was not made. Connect one under Your account, Armature account." + again
}

// person names the actor in a sentence that begins with it.
func (r *linkRun) person() string {
	if r.name == "" {
		return "The person who changed the requirement"
	}
	return r.name
}

// pending marks every link about to be put as not there yet, which it stays while Armature does not answer.
func (r *linkRun) pending(ctx context.Context, steps []LinkStep) error {
	var keys []string
	for _, step := range steps {
		if step.Want != nil {
			keys = append(keys, step.Key)
		}
	}
	_, err := r.tx.Exec(ctx, `
		INSERT INTO armature_remote_link (requirement_id, issue_key, state, updated_at)
		SELECT $1, k, 'pending', now() FROM unnest($2::text[]) AS k
		ON CONFLICT (requirement_id, issue_key) DO UPDATE SET state = 'pending', error = NULL, updated_at = now()`,
		r.requirementID, keys)
	return err
}

// fail marks a key failed with why, keeping what was recorded of its link so a later sync can still take it off.
func (r *linkRun) fail(ctx context.Context, step LinkStep, why string) error {
	_, err := r.tx.Exec(ctx, `
		INSERT INTO armature_remote_link (requirement_id, issue_key, state, error, updated_at)
		VALUES ($1, $2, 'failed', $3, now())
		ON CONFLICT (requirement_id, issue_key) DO UPDATE SET state = 'failed', error = EXCLUDED.error, updated_at = now()`,
		r.requirementID, step.Key, why)
	return err
}

func (r *linkRun) forget(ctx context.Context, key string) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM armature_remote_link WHERE requirement_id = $1 AND issue_key = $2`, r.requirementID, key)
	return err
}

func linkPath(key string) string {
	return "/issues/" + url.PathEscape(key) + "/remote-links"
}
