package reqifio

import (
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// wordPlace is the place an import at the project's top names in its identities.
const wordPlace = "project"

// wordFound is the requirement an earlier import made of a part, and how it was found.
type wordFound struct {
	target uuid.UUID
	by     WordMatch
}

// analyze reads an import's file against the project with the choices: a Word document is built anew with them.
func (j *job) analyze(w *world, choices map[string]string) *analysis {
	if j.Format != FormatDocx {
		return analyze(j.file, w, choices)
	}
	place, top := wordPlace, uuid.Nil
	if j.parentID != nil {
		place, top = j.parentID.String(), *j.parentID
	}
	build := buildWord(j.source, choices, w.project.Settings.LanguageOf(), j.FileName, place)
	return analyzeWith(build.file, w, choices, build, top)
}

// of is one way of finding a part again.
func (ids wordIDs) of(kind WordMatch) string {
	switch kind {
	case WordMatchKey:
		return ids.key
	case WordMatchNumber:
		return ids.number
	case WordMatchParagraph:
		return ids.paragraph
	}
	return ids.path
}

// readWord adds what the build found wrong and lost, and finds what earlier imports of the document made: each
// way of finding in turn over every part, so a stronger way wins a requirement over a weaker one.
func (a *analysis) readWord() {
	b := a.word
	a.preview.Problems = append(a.preview.Problems, b.problems...)
	for _, l := range b.losses {
		a.lose(LossKind(l.Kind), "", "%s", l.Message)
	}
	for _, s := range b.src.Styles {
		a.preview.Styles = append(a.preview.Styles, StyleUse{ID: s.ID, Name: s.Name, Count: s.Count})
	}
	a.preview.Tables = b.src.Tables
	if a.top != uuid.Nil {
		if e := a.world.existing[a.top]; e == nil || e.Kind != requirement.KindSection {
			a.problem("parentKey", "The section to import below is no longer in the project. Upload the document again below another section, or at the top.")
		}
	}
	index := map[WordMatch]map[string]uuid.UUID{}
	for _, kind := range WordMatches {
		index[kind] = map[string]uuid.UUID{}
	}
	for identifier, t := range a.world.ties["object"] {
		ids, ok := parseWordIdentity(identifier, b.ns)
		if !ok || a.world.existing[t.Target] == nil {
			continue
		}
		for _, kind := range WordMatches {
			if v := ids.of(kind); v != "" {
				index[kind][v] = t.Target
			}
		}
	}
	a.wordFound = map[string]wordFound{}
	claimed := map[uuid.UUID]bool{}
	for _, kind := range WordMatches {
		for _, o := range a.file.Objects {
			if _, done := a.wordFound[o.Identifier]; done {
				continue
			}
			v := b.parts[o.Identifier].ids.of(kind)
			if v == "" {
				continue
			}
			var candidates []uuid.UUID
			if kind == WordMatchKey {
				if id, ok := a.world.keys[v]; ok {
					candidates = append(candidates, id)
				}
			}
			if id, ok := index[kind][v]; ok {
				candidates = append(candidates, id)
			}
			for _, id := range candidates {
				if !claimed[id] && a.world.existing[id] != nil && id != a.top {
					claimed[id] = true
					a.wordFound[o.Identifier] = wordFound{target: id, by: kind}
					break
				}
			}
		}
	}
}

// tree is each part of the file as the import would make it, in order, at most MaxPreviewNodes.
func (a *analysis) tree() []PreviewNode {
	nodes := []PreviewNode{}
	add := func(n PreviewNode, p *objectPlan) {
		if len(nodes) == MaxPreviewNodes {
			return
		}
		if p != nil {
			n.Kind = NodeRequirement
			if p.Kind == requirement.KindSection {
				n.Kind = NodeSection
			}
			if p.Title != "" {
				n.Title = p.Title
			}
			if p.Existing != nil {
				n.MatchedBy, n.ExistingKey = p.MatchedBy, p.Existing.Key
			}
		}
		nodes = append(nodes, n)
	}
	if a.word != nil {
		for _, n := range a.word.nodes {
			add(PreviewNode{Depth: n.depth, Kind: n.kind, Title: n.title, Source: n.source, Losses: n.losses}, a.byRef[n.ref])
		}
		return nodes
	}
	depth := map[string]int{}
	for _, p := range a.plans {
		d := 0
		if p.Parent != "" {
			d = depth[p.Parent] + 1
		}
		depth[p.Ref] = d
		source := p.Where
		if o := a.index.objects[p.Ref]; o != nil {
			source = nameOf(o.Identity)
		}
		add(PreviewNode{Depth: d, Title: p.Where, Source: source, Losses: a.lossesAt[p.Where]}, p)
	}
	return nodes
}
