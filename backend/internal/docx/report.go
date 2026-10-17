package docx

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// A review session's report (#204): who took part and how far, each requirement's decisions round by round, its
// open threads, the rounds and the baseline it took.

// reportShares are the widths of the participants' and the rounds' tables, in percent of the text.
var (
	participantShares = []int{40, 20, 20, 20}
	roundShares       = []int{12, 14, 18, 56}
)

// ReportBytes writes a session's report as a .docx, its added words in lang.
func ReportBytes(ctx context.Context, rep *review.Report, exportedBy string, at time.Time, lang string) ([]byte, error) {
	w := newWriter(ctx, lang, Reader{})
	w.b.WriteString(xml.Header)
	w.b.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `"><w:body>`)
	f := frame{width: textWidth}
	s := rep.Session
	w.para(f, paraProps{style: "Title"}, func() { w.text(s.Title, runProps{}) })
	w.para(f, paraProps{style: "Subtitle"}, func() { w.text(w.words.pick("Review session of ", "Review-Sitzung von ")+s.ProjectKey, runProps{}) })
	lines := []string{
		w.words.pick("Moderated by ", "Moderiert von ") + s.ModeratorName,
		w.words.pick("Opened ", "Eröffnet ") + w.words.day(s.OpenedAt),
	}
	if s.ClosedAt != nil {
		lines = append(lines, w.words.pick("Closed ", "Geschlossen ")+w.words.day(*s.ClosedAt))
	} else {
		lines = append(lines, w.words.pick("Still open", "Noch offen"))
	}
	if s.Deadline != nil {
		lines = append(lines, w.words.pick("Deadline ", "Frist ")+w.words.day(*s.Deadline))
	}
	lines = append(lines,
		fmt.Sprintf(w.words.pick("%d rounds; each requirement needed %d%% of the approvers", "%d Runden; jede Anforderung brauchte %d %% der Genehmigenden"), len(rep.Rounds), s.Quorum),
		fmt.Sprintf(w.words.pick("%d approved, %d rejected, %d pending of %d", "%d genehmigt, %d abgelehnt, %d offen von %d"),
			s.Counts.Approved, s.Counts.Rejected, s.Counts.Pending, s.Counts.Total))
	if rep.BaselineName != "" {
		lines = append(lines, w.words.pick("Baseline taken: ", "Genommene Baseline: ")+rep.BaselineName)
	}
	lines = append(lines, w.words.exported(exportedBy, w.words.day(at)))
	for _, line := range lines {
		w.para(f, paraProps{style: "CoverLine"}, func() { w.text(line, runProps{}) })
	}

	w.para(f, paraProps{style: "Heading1"}, func() { w.text(w.words.pick("Participants", "Teilnehmende"), runProps{}) })
	w.grid(f, participantShares, []string{w.words.pick("Person", "Person"), w.words.pick("Role", "Rolle"),
		w.words.pick("Decided", "Entschieden"), w.words.pick("Commented", "Kommentiert")}, func(row func(...string)) {
		for _, p := range s.Progress {
			role := w.words.pick("Approver", "Genehmigend")
			if p.Role == review.RoleReviewer {
				role = w.words.pick("Reviewer", "Prüfend")
			}
			row(p.Person.Name, role, fmt.Sprintf("%d / %d", p.Decided, p.Of), fmt.Sprintf("%d / %d", p.Commented, p.Of))
		}
	})

	w.para(f, paraProps{style: "Heading1"}, func() { w.text(w.words.pick("Rounds", "Runden"), runProps{}) })
	for _, r := range rep.Rounds {
		line := fmt.Sprintf(w.words.pick("Round %d, %s", "Runde %d, %s"), r.Number, w.words.day(r.StartedAt))
		if r.Comment != "" {
			line += ": " + r.Comment
		}
		w.para(f, paraProps{}, func() { w.text(line, runProps{}) })
	}

	w.para(f, paraProps{style: "Heading1"}, func() { w.text(w.words.pick("Requirements", "Anforderungen"), runProps{}) })
	for _, item := range rep.Items {
		w.para(f, paraProps{style: "Heading2", keepNext: true}, func() {
			w.text(strings.TrimSpace(item.Outline+" "+item.Requirement.Key+" "+item.Requirement.Title), runProps{})
		})
		w.grid(f, roundShares, []string{w.words.pick("Round", "Runde"), w.words.pick("Revision", "Revision"),
			w.words.pick("Outcome", "Ergebnis"), w.words.pick("Decisions", "Entscheidungen")}, func(row func(...string)) {
			for _, r := range item.Rounds {
				var said []string
				for _, d := range r.Decisions {
					how := w.words.pick("approved", "genehmigt")
					if d.Decision == review.DecisionReject {
						how = w.words.pick("rejected", "abgelehnt")
					}
					one := d.ReviewerName + " " + how
					if d.Comment != "" {
						one += ": " + d.Comment
					}
					said = append(said, one)
				}
				row(fmt.Sprint(r.Round), fmt.Sprint(r.Revision), w.words.state(r.State), strings.Join(said, "; "))
			}
		})
		if item.OpenThreads > 0 {
			w.para(f, paraProps{}, func() {
				w.text(fmt.Sprintf(w.words.pick("%d open comment threads.", "%d offene Kommentarstränge."), item.OpenThreads), runProps{italic: true})
			})
		}
	}
	w.b.WriteString(`<w:sectPr><w:pgSz w:w="` + itoa(pageWidth) + `" w:h="` + itoa(pageHeight) + `"/>`)
	w.b.WriteString(`<w:pgMar w:top="` + itoa(margin) + `" w:right="` + itoa(margin) + `" w:bottom="` + itoa(margin) + `" w:left="` + itoa(margin) + `" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`)
	w.b.WriteString(`</w:body></w:document>`)
	if w.err != nil {
		return nil, w.err
	}
	var buf bytes.Buffer
	core := coreProperties(&specdoc.Document{Title: s.Title, ProjectName: s.ProjectKey, ExportedBy: exportedBy, ExportedAt: at}, lang)
	if err := w.pack(&buf, []byte(w.b.String()), at, core, lang); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// grid writes a bordered table of the shares of the text's width, a bold header row and the rows rows hands it.
func (w *writer) grid(f frame, shares []int, header []string, rows func(row func(...string))) {
	widths := make([]int, len(shares))
	for i, s := range shares {
		widths[i] = f.width * s / 100
	}
	w.tableStart(f, widths, true)
	write := func(bold bool, fill string, cells ...string) {
		w.b.WriteString("<w:tr>")
		for i, c := range cells {
			w.cell(widths[i], 1, "", fill, func(inner frame) {
				w.para(inner, paraProps{}, func() { w.text(c, runProps{bold: bold}) })
			}, f, 0)
		}
		w.b.WriteString("</w:tr>")
	}
	write(true, neutralFill, header...)
	rows(func(cells ...string) { write(false, "", cells...) })
	w.tableEnd()
}

// state names how a review ended, in the reader's language.
func (w words) state(s review.State) string {
	switch s {
	case review.StateApproved:
		return w.pick("Approved", "Genehmigt")
	case review.StateRejected:
		return w.pick("Rejected", "Abgelehnt")
	case review.StatePending:
		return w.pick("Pending", "Offen")
	case review.StateWithdrawn:
		return w.pick("Withdrawn", "Zurückgezogen")
	}
	return w.pick("Superseded", "Überholt")
}

// ReportFileName names a session's report after its project, its title and the day.
func ReportFileName(rep *review.Report, at time.Time, ext string) string {
	name := rep.Session.ProjectKey
	if slug := strings.Trim(unslug.ReplaceAllString(strings.ToLower(rep.Session.Title), "-"), "-"); slug != "" {
		name += "-" + slug
	}
	return name + "-report-" + at.Format(time.DateOnly) + "." + ext
}
