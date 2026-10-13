import type { labelsEn } from "./labels.en";

/** The German strings of labels, held to the English shape. */
export const labelsDe: typeof labelsEn = {
  title: "Labels",
  add: "Label hinzufügen",
  placeholder: "Label eingeben",
  options: "Labels zur Auswahl",
  create: (name: string) => `${name} hinzufügen`,
  remove: (name: string) => `Label ${name} entfernen`,
  added: (name: string) => `Label ${name} hinzugefügt.`,
  removed: (name: string) => `Label ${name} entfernt.`,
  none: "Noch keine Labels.",
  loading: "Labels werden gesucht",
  noMatch: "Noch kein Label beginnt so.",
  invalid: "Verwenden Sie Buchstaben, Ziffern, Bindestriche, Unterstriche und Punkte, und beginnen Sie mit einem Buchstaben oder einer Ziffer.",
  tooLong: (max: number) => `Ein Label darf höchstens ${max} Zeichen haben.`,
  used: (requirements: number) => (requirements === 1 ? "an 1 Anforderung" : `an ${requirements} Anforderungen`),
  heading: (name: string) => `Label ${name}`,
  inProject: (project: string) => `Anforderungen in ${project} mit diesem Label`,
  everywhere: "Anforderungen in allen Projekten mit diesem Label",
  showEverywhere: "Alle Projekte zeigen",
  filtered: (name: string) => `Anforderungen mit dem Label ${name}`,
  clearFilter: "Alle Anforderungen zeigen",
  count: (total: number, from: number, to: number) =>
    total === 0
      ? "Keine Anforderungen"
      : total <= to && from === 1
        ? total === 1
          ? "1 Anforderung"
          : `${total} Anforderungen`
        : `${from} bis ${to} von ${total} Anforderungen`,
  emptyTitle: "Keine Anforderung trägt dieses Label",
  emptyBody: "Vergeben Sie das Label an der Anforderung selbst, oder prüfen Sie die Schreibweise.",
  failed: "Die Anforderungen mit diesem Label konnten nicht geladen werden. Versuchen Sie es gleich noch einmal.",
  changed: (who: string, when: string) => (who ? `${who} hat sie am ${when} geändert` : `Geändert am ${when}`),
  paging: "Seiten dieser Liste",
  previous: "Zurück",
  next: "Weiter",
  pageOf: (page: number, pages: number) => `Seite ${page} von ${pages}`,
};
