package docx

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// A project's risk report (#261): its scheme, how many risks stand at each level, and each risk with its ratings and
// its mitigations.

// The widths of the report's tables, in percent of the text.
var (
	levelShares      = []int{40, 20, 20, 20}
	ratingShares     = []int{28, 16, 16, 16, 10, 14}
	mitigationShares = []int{44, 14, 14, 14, 14}
)

// RiskReportBytesWith writes a project's risk report, into a Word template when one is given (#214).
func RiskReportBytesWith(ctx context.Context, rep *risk.Report, exportedBy string, at time.Time, lang string, t *Template, values Fill) ([]byte, error) {
	w := newWriter(ctx, lang, Reader{})
	w.layout, w.fill = t, values
	if t == nil {
		w.b.WriteString(xml.Header)
		w.b.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `"><w:body>`)
	}
	f := frame{width: w.pageText()}
	title := w.words.pick("Risk report", "Risikobericht")
	w.para(f, paraProps{style: "Title"}, func() { w.text(title, runProps{}) })
	w.para(f, paraProps{style: "Subtitle"}, func() { w.text(rep.ProjectName+" ("+rep.ProjectKey+")", runProps{}) })
	method := w.words.pick("Levels by a matrix of severity and probability", "Stufen nach einer Matrix aus Schwere und Wahrscheinlichkeit")
	if rep.Scheme.Method == risk.MethodRPN {
		method = w.words.pick("Levels by priority number", "Stufen nach Risikoprioritätszahl")
	}
	for _, line := range []string{method, fmt.Sprintf(w.words.pick("%d risks", "%d Risiken"), len(rep.Risks)), w.words.exported(exportedBy, w.words.day(at))} {
		w.para(f, paraProps{style: "CoverLine"}, func() { w.text(line, runProps{}) })
	}

	w.para(f, paraProps{style: "Heading1"}, func() { w.text(w.words.pick("Levels", "Stufen"), runProps{}) })
	w.grid(f, levelShares, []string{w.words.pick("Level", "Stufe"), w.words.pick("Acceptable", "Akzeptabel"),
		w.words.pick("Before mitigation", "Vor der Minderung"), w.words.pick("In effect", "Wirksam")}, func(row func(...string)) {
		for _, l := range rep.Levels {
			row(l.Level.Name, w.words.yesNo(l.Level.Acceptable), strconv.Itoa(l.Before), strconv.Itoa(l.Effective))
		}
	})
	if rep.Unrated > 0 {
		w.para(f, paraProps{}, func() {
			w.text(fmt.Sprintf(w.words.pick("%d risks are not rated yet.", "%d Risiken sind noch nicht bewertet."), rep.Unrated), runProps{italic: true})
		})
	}

	w.para(f, paraProps{style: "Heading1"}, func() { w.text(w.words.pick("Risks", "Risiken"), runProps{}) })
	for _, r := range rep.Risks {
		w.riskEntry(f, r)
	}
	if w.err != nil {
		return nil, w.err
	}
	var body []byte
	if t != nil {
		body = w.framed(w.b.String())
	} else {
		w.b.WriteString(defaultSectPr())
		w.b.WriteString(`</w:body></w:document>`)
		body = []byte(w.b.String())
	}
	var buf bytes.Buffer
	core := coreProperties(&specdoc.Document{Title: title, ProjectName: rep.ProjectName, ExportedBy: exportedBy, ExportedAt: at}, lang)
	if err := w.pack(&buf, body, at, core, lang); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// riskEntry writes one risk: its heading, its state and level, its rating before and after, and its mitigations.
func (w *writer) riskEntry(f frame, r risk.Row) {
	w.para(f, paraProps{style: "Heading2", keepNext: true}, func() { w.text(r.Risk.Key+" "+r.Risk.Title, runProps{}) })
	v := r.Rating
	meta := r.Risk.Type.Name + ", " + w.words.riskState(v.State)
	if v.Level != nil {
		meta += ", " + w.words.pick("stands at ", "steht bei ") + v.Level.Name
	}
	w.para(f, paraProps{style: "EntryMeta"}, func() { w.text(meta, runProps{}) })
	number := func(n *int) string {
		if n == nil {
			return "-"
		}
		return strconv.Itoa(*n)
	}
	level := func(l *requirement.RiskLevel) string {
		if l == nil {
			return "-"
		}
		return l.Name
	}
	w.grid(f, ratingShares, []string{w.words.pick("Rating", "Bewertung"), w.words.pick("Severity", "Schwere"), w.words.pick("Probability", "Wahrscheinlichkeit"),
		w.words.pick("Detection", "Entdeckung"), w.words.pick("RPN", "RPZ"), w.words.pick("Level", "Stufe")}, func(row func(...string)) {
		row(w.words.pick("Before", "Vorher"), number(v.Before.Severity), number(v.Before.Probability), number(v.Before.Detection), number(v.BeforeRPN), level(v.BeforeLevel))
		row(w.words.pick("After", "Nachher"), number(v.After.Severity), number(v.After.Probability), number(v.After.Detection), number(v.AfterRPN), level(v.AfterLevel))
	})
	if len(r.Mitigations) == 0 {
		w.para(f, paraProps{}, func() { w.text(w.words.pick("Nothing mitigates it.", "Nichts mindert es."), runProps{italic: true}) })
		return
	}
	w.grid(f, mitigationShares, []string{w.words.pick("Mitigated by", "Gemindert durch"), w.words.pick("Implemented", "Umgesetzt"),
		w.words.pick("Verified", "Verifiziert"), w.words.pick("Suspect", "Verdächtig"), w.words.pick("In effect", "Wirksam")}, func(row func(...string)) {
		for _, m := range r.Mitigations {
			source := m.Source.Key + " " + m.Source.Title
			if m.Source.Hidden {
				source = w.words.pick("A requirement you may not view", "Eine Anforderung, die Sie nicht sehen dürfen")
			}
			row(source, w.words.coverage(m.Implemented), w.words.coverage(m.Verified), w.words.yesNo(m.Suspect), w.words.yesNo(m.InEffect))
		}
	})
}

// riskState names how far a risk is mitigated, in the reader's language.
func (w words) riskState(s *requirement.RiskState) string {
	if s == nil {
		return ""
	}
	switch *s {
	case requirement.Mitigated:
		return w.pick("mitigated", "gemindert")
	case requirement.Claimed:
		return w.pick("mitigation claimed", "Minderung behauptet")
	}
	return w.pick("unmitigated", "ungemindert")
}

// coverage names a coverage level, in the reader's language.
func (w words) coverage(l trace.Level) string {
	switch l {
	case trace.LevelFull:
		return w.pick("Full", "Voll")
	case trace.LevelPartial:
		return w.pick("Partial", "Teilweise")
	case trace.LevelNA:
		return w.pick("Not asked", "Nicht gefragt")
	}
	return w.pick("None", "Keine")
}

// yesNo is a yes or a no, in the reader's language.
func (w words) yesNo(b bool) string {
	if b {
		return w.pick("Yes", "Ja")
	}
	return w.pick("No", "Nein")
}

// RiskReportFileName names a risk report after its project and the day.
func RiskReportFileName(rep *risk.Report, at time.Time, ext string) string {
	return rep.ProjectKey + "-risk-report-" + at.Format(time.DateOnly) + "." + ext
}
