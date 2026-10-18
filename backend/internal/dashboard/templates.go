package dashboard

import "slices"

// templates are the first dashboards a project starts from, kept in code as Armature keeps its built-in ones.
var templates = []Template{
	{Key: "overview", Name: "Overview", Description: "Where the project's requirements stand: their status, coverage, review, suspect links, quality and changes.",
		Widgets: []WidgetInput{
			{Kind: Breakdown, Title: "By status", Config: Config{GroupBy: ByStatus, Shape: Donut}},
			{Kind: Coverage, Title: "Coverage"},
			{Kind: Review, Title: "Review"},
			{Kind: Suspects, Title: "Suspect links"},
			{Kind: Quality, Title: "Quality"},
			{Kind: Changes, Title: "Changes", Config: Config{Interval: Week, Series: []Series{Created, Revised, StatusChanged}}},
		}},
	{Key: "verification", Name: "Verification", Description: "How far the requirements are verified, by tests that pass on the current revision.",
		Widgets: []WidgetInput{
			{Kind: Coverage, Title: "Coverage"},
			{Kind: Count, Title: "Failing tests", Config: Config{Source: Source{Query: "testResult = failed"}}},
			{Kind: Count, Title: "Outdated tests", Config: Config{Source: Source{Query: "testOutdated > 0"}}},
			{Kind: List, Title: "Not verified", Config: Config{Source: Source{Query: "verified = none"}, Rows: DefaultListRows}},
		}},
}

// Templates answers the templates a first dashboard is made from.
func Templates() []Template { return slices.Clone(templates) }

func templateOf(key string) (Template, bool) {
	i := slices.IndexFunc(templates, func(t Template) bool { return t.Key == key })
	if i < 0 {
		return Template{}, false
	}
	return templates[i], true
}
