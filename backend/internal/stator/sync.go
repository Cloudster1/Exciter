package stator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// Link sync (#42), Armature's of #27 pointed at pages: the worker brings the remote links of one requirement on
// Stator's pages in line with its page links, as the person whose change it was.

const (
	// TopicLinks names a requirement whose remote links to sync; the database writes it.
	TopicLinks = "stator.links"
	// RequirementPath and IconPath are where a link on a page leads, and its icon, under Exciter's origin.
	RequirementPath = "/r/"
	IconPath        = "/favicon-32.png"
)

// LinksEvent is what a stator.links event carries.
type LinksEvent struct {
	RequirementID uuid.UUID `json:"requirementId"`
	ActorID       uuid.UUID `json:"actorId"`
}

// LinkTitle is how a requirement is titled on a page: its key, its title and how it relates, cut to Stator's bound.
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

// WantedLink is the remote link a requirement should have on one page.
type WantedLink struct {
	URL   string
	Title string
}

// LinkRecord is what the last sync did on one page.
type LinkRecord struct {
	PageID   uuid.UUID
	RemoteID *uuid.UUID
	URL      string
	Title    string
	State    SyncState
}

// LinkStep is one change to make in Stator: Want set puts the link, nil takes Have's off the page.
type LinkStep struct {
	PageID uuid.UUID
	Want   *WantedLink
	Have   *LinkRecord
}

// PlanLinks compares what a requirement should have in Stator with what the last sync recorded, by page. A link
// synced as wanted is left alone; a failed or pending one is tried again.
func PlanLinks(want map[uuid.UUID]WantedLink, have []LinkRecord) []LinkStep {
	held := make(map[uuid.UUID]*LinkRecord, len(have))
	for i := range have {
		held[have[i].PageID] = &have[i]
	}
	var steps []LinkStep
	for page, w := range want {
		rec := held[page]
		if rec != nil && rec.State == SyncSynced && rec.RemoteID != nil && rec.URL == w.URL && rec.Title == w.Title {
			continue
		}
		steps = append(steps, LinkStep{PageID: page, Want: &WantedLink{URL: w.URL, Title: w.Title}, Have: rec})
	}
	for page, rec := range held {
		if _, ok := want[page]; !ok {
			steps = append(steps, LinkStep{PageID: page, Have: rec})
		}
	}
	slices.SortFunc(steps, func(a, b LinkStep) int { return strings.Compare(a.PageID.String(), b.PageID.String()) })
	return steps
}

// WantedLinks are the remote links a live requirement should have: one per page it is traced to, titled with the
// first relation in Relations' order. Broken and detached links want none, and a trashed requirement none.
func WantedLinks(appURL string, req RequirementState, links []LinkedPage) map[uuid.UUID]WantedLink {
	want := map[uuid.UUID]WantedLink{}
	if !req.Found || req.Trashed {
		return want
	}
	first := map[uuid.UUID]Relation{}
	for _, l := range links {
		if l.Broken || l.Detached {
			continue
		}
		if had, ok := first[l.PageID]; !ok || slices.Index(Relations, l.Relation) < slices.Index(Relations, had) {
			first[l.PageID] = l.Relation
		}
	}
	target := RequirementURL(appURL, req.ID)
	for page, relation := range first {
		want[page] = WantedLink{URL: target, Title: LinkTitle(req.Key, req.Title, relation)}
	}
	return want
}

// RequirementURL is where a remote link on a page leads: the requirement by its id, whatever its key becomes.
func RequirementURL(appURL string, id uuid.UUID) string {
	return strings.TrimSuffix(appURL, "/") + RequirementPath + id.String()
}

// RequirementState is what a sync needs of a requirement; Found is false once it is purged.
type RequirementState struct {
	ID      uuid.UUID
	Found   bool
	Key     string
	Title   string
	Trashed bool
}

// LinkedPage is one of a requirement's page links as the sync sees it.
type LinkedPage struct {
	PageID   uuid.UUID
	Relation Relation
	Broken   bool
	Detached bool
}

// RemoteLinkRequest is what link sync sends Stator to put a requirement on a page.
type RemoteLinkRequest struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Source  string `json:"source"`
	IconURL string `json:"iconUrl"`
}

// LinkSync is the worker's handler for stator.links.
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
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('stator-links:' || $1::text, 0))`, in.RequirementID); err != nil {
			return err
		}
		takes, req, links, have, err := l.read(ctx, tx, in.RequirementID)
		if err != nil || !takes {
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
		// Stator not answering the refresh of a grant from a sign-in is Stator away, tried again later.
		if run.status == StatusUnreachable {
			retry = ErrUnreachable
			return nil
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

// read is what one sync needs; takes is false while the installation has no Stator that takes remote links.
func (l *LinkSync) read(ctx context.Context, tx db.DBTX, id uuid.UUID) (bool, RequirementState, []LinkedPage, []LinkRecord, error) {
	req := RequirementState{ID: id}
	var takes bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stator_connection WHERE remote_links)`).Scan(&takes); err != nil || !takes {
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
		SELECT page_id, relation, broken_at IS NOT NULL, detached_at IS NOT NULL
		FROM stator_page_link WHERE requirement_id = $1`, id)
	if err != nil {
		return false, req, nil, nil, err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LinkedPage])
	if err != nil {
		return false, req, nil, nil, err
	}
	rows, err = tx.Query(ctx, `
		SELECT page_id, remote_link_id, COALESCE(url, ''), COALESCE(title, ''), state
		FROM stator_remote_link WHERE requirement_id = $1`, id)
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
