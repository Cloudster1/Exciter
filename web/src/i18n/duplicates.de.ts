import type { duplicatesEn } from "./duplicates.en";

/** The German strings of duplicates, held to the English shape. */
export const duplicatesDe: typeof duplicatesEn = {
  alike: (percent: string) => `${percent} ähnlich`,
  library: "Bibliothek",
  libraryOf: (projectKey: string) => `Aus der Bibliothek ${projectKey}`,
  newTab: "(öffnet in einem neuen Tab)",

  hint: {
    title: "Ähnliche Anforderungen",
    intro: "Schon geschrieben? Diese sagen weitgehend dasselbe.",
    count: (n: number) => (n === 0 ? "" : n === 1 ? "1 ähnliche Anforderung." : `${n} ähnliche Anforderungen.`),
    reuse: "Stattdessen wiederverwenden...",
    reuseLabel: (key: string) => `Stattdessen ${key} wiederverwenden`,
  },

  panel: {
    title: "Ähnliche Anforderungen",
    none: "Keine Anforderung ist so ähnlich, wie die Schwelle des Projekts verlangt.",
  },

  page: {
    title: "Duplikate",
    intro:
      "Anforderungen dieses Projekts, die weitgehend dasselbe sagen, die ähnlichsten zuerst. Verwerfen Sie ein Paar, verknüpfen Sie die beiden oder führen Sie sie zu einer zusammen.",
    threshold: (percent: string) => `Aufgeführt ab ${percent} Ähnlichkeit, wie in den Qualitätseinstellungen des Projekts festgelegt.`,
    filters: "Paare eingrenzen",
    type: "Typ",
    anyType: "Jeder Typ",
    section: "Abschnitt",
    sectionHint: "Der Schlüssel eines Abschnitts, unter dem eine der beiden Seiten steht.",
    filter: "Eingrenzen",
    queryLabel: "Abfrage, die eine der beiden Seiten erfüllt",
    count: (n: number) => (n === 0 ? "Keine Paare." : n === 1 ? "1 Paar angezeigt." : `${n} Paare angezeigt.`),
    none: "Keine wahrscheinlichen Duplikate.",
    noneHint: "Keine zwei Anforderungen sind so ähnlich, wie die Schwelle verlangt, oder über jedes Paar ist entschieden.",
    more: "Mehr",
    pair: (a: string, b: string) => `${a} und ${b}`,
    revision: (n: number) => `Revision ${n}`,
    differences: "Was sich unterscheidet",
    onlyIn: (key: string) => `Nur in ${key}:`,
    same: "Die Anforderungstexte lauten gleich.",
    noStatement: "Kein Anforderungstext.",
    readOnly: "Sie können die Paare lesen. Um über sie zu entscheiden, brauchen Sie das Recht, das Projekt zu bearbeiten.",
    stale: "Eine der beiden hat sich geändert, seit die Paare gelesen wurden. Lesen Sie sie erneut und entscheiden Sie dann.",
    readAgain: "Paare erneut lesen",
  },

  dismiss: {
    action: "Verwerfen...",
    label: (a: string, b: string) => `${a} und ${b} verwerfen`,
    title: "Paar verwerfen",
    intro: (a: string, b: string) => `${a} und ${b} sagen nicht dasselbe. Das Paar kommt zurück, sobald eine der beiden überarbeitet wird.`,
    reason: "Begründung",
    reasonHint: "Sagen Sie, warum die beiden nicht dasselbe sind. Die Begründung bleibt bei der Entscheidung.",
    send: "Verwerfen",
    done: (a: string, b: string) => `${a} und ${b} werden nicht mehr als Duplikate aufgeführt.`,
  },

  link: {
    action: "Verknüpfen...",
    label: (a: string, b: string) => `${a} und ${b} verknüpfen`,
    title: "Die beiden verknüpfen",
    intro: "Die beiden hängen zusammen, sind aber nicht dasselbe. Das Paar bleibt fern, solange die Verknüpfung besteht.",
    way: "Verknüpfung",
    reads: (a: string, phrase: string, b: string) => `${a} ${phrase} ${b}`,
    none: "Das Projekt nutzt keine Verknüpfungsart. Wählen Sie in seinen Einstellungen die Arten, die es nutzt.",
    send: "Verknüpfen",
    done: (a: string, b: string) => `${a} und ${b} sind verknüpft.`,
  },

  merge: {
    action: "Zusammenführen...",
    label: (a: string, b: string) => `${a} und ${b} zusammenführen`,
    title: "Die beiden zusammenführen",
    intro:
      "Die nicht behaltene kommt in den Papierkorb, und jede ihrer Verknüpfungen wird an der behaltenen neu angelegt. Wird sie aus dem Papierkorb wiederhergestellt, kommen ihre eigenen Verknüpfungen mit zurück.",
    keep: "Behalten",
    trashed: (key: string) => `${key} kommt in den Papierkorb.`,
    comment: "Kommentar",
    commentHint: "Freiwillig. Er bleibt bei der Entscheidung.",
    send: "Zusammenführen",
    done: (kept: string, trashed: string) => `${trashed} liegt im Papierkorb, und ${kept} ist behalten.`,
    links: "Ihre Verknüpfungen",
    noLinks: "Sie hatte keine Verknüpfungen zu übertragen.",
    outgoing: (type: string, other: string) => `${type} zu ${other}`,
    incoming: (type: string, other: string) => `${type} von ${other}`,
    moved: "An der behaltenen neu angelegt.",
    skipped: "Übersprungen:",
    reasons: {
      self: "Sie würde die behaltene Anforderung mit sich selbst verknüpfen.",
      duplicate: "Die behaltene Anforderung hat diese Verknüpfung schon.",
      retired: "Ihre Verknüpfungsart ist stillgelegt.",
      refused: "Ihre Verknüpfungsart erlaubt sie an der behaltenen Anforderung nicht.",
    },
    close: "Fertig",
  },

  settings: {
    title: "Duplikate",
    intro:
      "Wie ähnlich zwei Anforderungen sein müssen, damit die Seite der Duplikate und der Hinweis beim Schreiben sie aufführen. Niedriger führt mehr Paare auf, höher nur die ähnlichsten.",
    threshold: "Schwelle der Ähnlichkeit in Prozent",
    range: (min: string, max: string) => `Von ${min} bis ${max}, in Schritten von 5.`,
    save: "Schwelle speichern",
    saved: "Die Schwelle ist gespeichert.",
    readOnly: "Nur die Administratoren des Projekts legen die Schwelle fest.",
  },
};
