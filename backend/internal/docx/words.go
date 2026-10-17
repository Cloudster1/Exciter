package docx

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// words are what a document says beyond the specification's own, in the reader's language.
type words struct {
	contents, rationale, links, unlocked, somebody, yes, no string
	fields                                                  map[string]string
	priorities                                              map[string]string
	months                                                  [12]string
	german                                                  bool
}

var english = words{
	contents: "Contents", rationale: "Rationale", links: "Links", unlocked: "The baseline is not locked; it may still be renamed or deleted.",
	somebody: "somebody", yes: "Yes", no: "No",
	fields:     map[string]string{specdoc.FieldType: "Type", specdoc.FieldStatus: "Status", specdoc.FieldOwner: "Owner", specdoc.FieldPriority: "Priority", specdoc.FieldLabels: "Labels"},
	priorities: map[string]string{"must": "Must", "should": "Should", "could": "Could", "wont": "Won't"},
	months:     [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
}

var german = words{
	contents: "Inhalt", rationale: "Begründung", links: "Verknüpfungen", unlocked: "Die Baseline ist nicht gesperrt; sie kann noch umbenannt oder gelöscht werden.",
	somebody: "jemand", yes: "Ja", no: "Nein",
	fields:     map[string]string{specdoc.FieldType: "Typ", specdoc.FieldStatus: "Status", specdoc.FieldOwner: "Verantwortlich", specdoc.FieldPriority: "Priorität", specdoc.FieldLabels: "Labels"},
	priorities: map[string]string{"must": "Muss", "should": "Soll", "could": "Kann", "wont": "Nicht"},
	months:     [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	german:     true,
}

func wordsFor(lang string) words {
	if strings.EqualFold(lang, "de") {
		return german
	}
	return english
}

func (w words) pick(en, de string) string {
	if w.german {
		return de
	}
	return en
}

// of says what a document is of, under its title.
func (w words) of(doc *specdoc.Document) string {
	project := doc.ProjectName + " (" + doc.ProjectKey + ")"
	switch doc.Kind {
	case specdoc.KindSection:
		return w.pick("Section "+doc.SectionKey+" of "+project, "Abschnitt "+doc.SectionKey+" von "+project)
	case specdoc.KindBaseline:
		return w.pick("Baseline of "+project, "Baseline von "+project)
	}
	return w.pick("Specification of "+project, "Spezifikation von "+project)
}

func (w words) taken(day, by string) string {
	return w.pick(fmt.Sprintf("Taken on %s by %s.", day, by), fmt.Sprintf("Erstellt am %s von %s.", day, by))
}

func (w words) locked(day string) string {
	return w.pick(fmt.Sprintf("Locked on %s; it never changes.", day), fmt.Sprintf("Gesperrt am %s; sie ändert sich nie.", day))
}

// signing says where a baseline's signing stands (#205).
func (w words) signing(signed bool, n, of int) string {
	if signed {
		return w.pick(fmt.Sprintf("Signed, with %d of %d signatures.", n, of), fmt.Sprintf("Unterschrieben, mit %d von %d Unterschriften.", n, of))
	}
	return w.pick(fmt.Sprintf("Being signed: %d of %d signatures so far.", n, of), fmt.Sprintf("In Unterschrift: bisher %d von %d Unterschriften.", n, of))
}

func (w words) exported(by, day string) string {
	return w.pick(fmt.Sprintf("Exported by %s on %s.", by, day), fmt.Sprintf("Exportiert von %s am %s.", by, day))
}

func (w words) holds(n int) string {
	if n == 1 {
		return w.pick("It holds 1 requirement or section.", "Es enthält 1 Anforderung oder 1 Abschnitt.")
	}
	return w.pick(fmt.Sprintf("It holds %d requirements and sections.", n), fmt.Sprintf("Es enthält %d Anforderungen und Abschnitte.", n))
}

func (w words) keyAndRevision(key string, revision int) string {
	return key + " · " + w.pick("revision ", "Revision ") + strconv.Itoa(revision)
}

func (w words) truncated(n, total int) string {
	return w.pick(fmt.Sprintf("This document holds the first %d of the %d requirements. Export a section to hold the rest.", n, total),
		fmt.Sprintf("Dieses Dokument enthält die ersten %d der %d Anforderungen. Exportieren Sie einen Abschnitt, um den Rest zu erhalten.", n, total))
}

func (w words) pictureMissing(alt string) string {
	if alt == "" {
		return w.pick("A picture that could not be read is left out.", "Ein Bild, das nicht gelesen werden konnte, fehlt hier.")
	}
	return w.pick("A picture that could not be read is left out: "+alt, "Ein Bild, das nicht gelesen werden konnte, fehlt hier: "+alt)
}

// property names a property: a field in the reader's language, an attribute by its own name.
func (w words) property(p specdoc.Property) string {
	if p.Field != "" {
		return w.fields[p.Field]
	}
	return p.Name
}

// value is a property's value as text, a priority in the reader's language.
func (w words) value(p specdoc.Property) string {
	if p.Field == specdoc.FieldPriority {
		if name, ok := w.priorities[p.Text]; ok {
			return name
		}
	}
	return p.Text
}

func (w words) flag(b bool) string {
	if b {
		return w.yes
	}
	return w.no
}

// day writes a day in words, as the reader's language writes it.
func (w words) day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.UTC()
	month := w.months[t.Month()-1]
	if w.german {
		return strconv.Itoa(t.Day()) + ". " + month + " " + strconv.Itoa(t.Year())
	}
	return month + " " + strconv.Itoa(t.Day()) + ", " + strconv.Itoa(t.Year())
}
