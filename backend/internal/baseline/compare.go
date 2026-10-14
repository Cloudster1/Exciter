package baseline

import (
	"cmp"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// held is a requirement as one side of a comparison holds it.
type held struct {
	ID       uuid.UUID
	Number   int
	Revision int
	// Title is the revision's, empty for a requirement purged since.
	Title    string
	Path     []int32
	ParentID *uuid.UUID
	Rank     string
	Status   *requirement.StatusRef
}

func (h held) side() *ItemSide {
	return &ItemSide{Revision: h.Revision, Title: h.Title, Outline: requirement.OutlineOf(h.Path), Status: h.Status}
}

func statusOf(s *requirement.StatusRef) uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return s.ID
}

// Classify says how each requirement differs from one side to the other: added, removed, or changed by a revision,
// a move to another parent or place, or a status. One that differs in none of these is left out.
func Classify(projectKey string, from, to []held) []ItemChange {
	before := make(map[uuid.UUID]held, len(from))
	for _, h := range from {
		before[h.ID] = h
	}
	type placed struct {
		change ItemChange
		path   []int32
	}
	var out []placed
	seen := make(map[uuid.UUID]bool, len(to))
	for _, now := range to {
		seen[now.ID] = true
		key := requirement.Key(projectKey, now.Number)
		was, ok := before[now.ID]
		if !ok {
			out = append(out, placed{ItemChange{Key: key, Change: Added, To: now.side()}, now.Path})
			continue
		}
		c := ItemChange{
			Key: key, Change: Changed, From: was.side(), To: now.side(),
			Revised:       was.Revision != now.Revision,
			Moved:         !equalParent(was.ParentID, now.ParentID) || was.Rank != now.Rank,
			StatusChanged: statusOf(was.Status) != statusOf(now.Status),
		}
		if c.Revised || c.Moved || c.StatusChanged {
			out = append(out, placed{c, now.Path})
		}
	}
	for _, was := range from {
		if !seen[was.ID] {
			out = append(out, placed{ItemChange{Key: requirement.Key(projectKey, was.Number), Change: Removed, From: was.side()}, was.Path})
		}
	}
	slices.SortStableFunc(out, func(a, b placed) int { return slices.Compare(a.path, b.path) })
	changes := make([]ItemChange, len(out))
	for i, p := range out {
		changes[i] = p.change
	}
	return changes
}

func equalParent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// heldLink is a trace link one side holds.
type heldLink struct {
	TypeID, SourceID, TargetID uuid.UUID
	TypeName                   string
}

// DiffLinks lists the links one side has and the other has not, removed ones first, each in the order of its ends'
// keys; keyOf names a requirement.
func DiffLinks(from, to []heldLink, keyOf func(uuid.UUID) string) []LinkChange {
	type id struct{ t, s, d uuid.UUID }
	idOf := func(l heldLink) id { return id{l.TypeID, l.SourceID, l.TargetID} }
	out := []LinkChange{}
	for _, pass := range []struct {
		change      Change
		these, them []heldLink
	}{{Removed, from, to}, {Added, to, from}} {
		other := map[id]bool{}
		for _, l := range pass.them {
			other[idOf(l)] = true
		}
		var found []LinkChange
		for _, l := range pass.these {
			if !other[idOf(l)] {
				found = append(found, LinkChange{Change: pass.change, TypeName: l.TypeName, SourceKey: keyOf(l.SourceID), TargetKey: keyOf(l.TargetID)})
			}
		}
		slices.SortFunc(found, func(a, b LinkChange) int {
			return cmp.Or(cmp.Compare(a.SourceKey, b.SourceKey), cmp.Compare(a.TargetKey, b.TargetKey), cmp.Compare(a.TypeName, b.TypeName))
		})
		out = append(out, found...)
	}
	return out
}

// heldExternal is an Armature link one side holds.
type heldExternal struct {
	RequirementID      uuid.UUID
	IssueKey, Relation string
}

// DiffExternal lists the Armature links one side has and the other has not, removed ones first.
func DiffExternal(from, to []heldExternal, keyOf func(uuid.UUID) string) []ExternalChange {
	out := []ExternalChange{}
	for _, pass := range []struct {
		change      Change
		these, them []heldExternal
	}{{Removed, from, to}, {Added, to, from}} {
		var found []ExternalChange
		for _, l := range pass.these {
			if !slices.Contains(pass.them, l) {
				found = append(found, ExternalChange{Change: pass.change, RequirementKey: keyOf(l.RequirementID), IssueKey: l.IssueKey, Relation: l.Relation})
			}
		}
		slices.SortFunc(found, func(a, b ExternalChange) int {
			return cmp.Or(cmp.Compare(a.RequirementKey, b.RequirementKey), cmp.Compare(a.IssueKey, b.IssueKey), cmp.Compare(a.Relation, b.Relation))
		})
		out = append(out, found...)
	}
	return out
}
