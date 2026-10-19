import type { evidenceEn } from "./evidence.en";

/** The German strings of verification evidence, held to the English shape. */
export const evidenceDe: typeof evidenceEn = {
  results: {
    title: "Testergebnisse",
    none: "Noch kein Testlauf nennt diese Anforderung. Ein Test nennt sie mit [verifies KEY] in seinem Namen.",
    counting: (branch: string) => `Auf ${branch}, der zählt`,
    other: (branch: string) => `Auf ${branch}`,
    outcome: { passed: "Bestanden", failed: "Fehlgeschlagen", skipped: "Übersprungen", errored: "Fehler" },
    outdated: (revision: number) => `Veraltet: gegen Revision ${revision} gelaufen`,
    outdatedHint: "Die Anforderung wurde seitdem überarbeitet. Führen Sie den Test erneut aus, damit das Ergebnis zu ihrem jetzigen Text gehört.",
    commit: (sha: string) => `Commit ${sha}`,
    run: "Lauf",
    inArmature: (issueKey: string) => `In Armature, ${issueKey}`,
    job: (job: string) => `Job ${job}`,
    when: (at: string) => `am ${at}`,
  },
  settings: {
    title: "Nachweise",
    intro:
      "Testergebnisse kommen je Branch aus der CI. Der zählende Branch ist der, dessen Ergebnisse für Abdeckung, Matrix und Abfragen zählen; die anderen erscheinen an jeder Anforderung.",
    branch: "Zählender Branch",
    save: "Speichern",
    saved: "Gespeichert.",
    readOnly: "Nur die Administratoren des Projekts benennen den zählenden Branch.",
  },
  sources: {
    legend: "Verifiziert durch",
    implementedLegend: "Implementiert durch",
    links: "Verknüpfungen",
    tests: "Testergebnisse",
    code: "Code",
    runs: "Testdurchführungen",
    hint: "Was für eine Anforderung dieses Typs zählt: Verknüpfungen, bestandene Testergebnisse auf dem zählenden Branch, Code-Tags mit ihrer aktuellen Revision, Testfälle, deren letztes Ergebnis in einer Testdurchführung bestanden ist, oder mehreres davon.",
  },
  matrix: {
    tests: "Tests",
    code: "Code",
    outdated: "veraltet",
  },
  chains: {
    tests: "Tests",
  },
  code: {
    title: "Code",
    none: "Noch kein Scan taggt diese Anforderung. Ein Quellcode-Kommentar taggt sie mit [implements KEY] oder [verifies KEY].",
    relation: { implements: "Implementiert", verifies: "Verifiziert" },
    at: (path: string, line: number) => `${path}:${line}`,
    outdated: (revision: number) => `Veraltet: gegen Revision ${revision} geschrieben`,
    outdatedHint: "Die Anforderung wurde seitdem überarbeitet. Prüfen Sie den Code und nennen Sie im Tag die aktuelle Revision.",
    commit: (sha: string) => `bei ${sha}`,
  },
  codeTags: {
    title: "Code-Tags",
    intro: (branch: string) =>
      `Tags im Code auf ${branch}, die eine ältere Revision ihrer Anforderung nennen, oder eine Anforderung im Papierkorb oder gar keine.`,
    none: "Jedes Tag auf dem zählenden Branch nennt die aktuelle Revision einer Anforderung.",
    state: "Zeigen",
    states: { "": "Veraltet und verwaist", outdated: "Veraltet", orphaned: "Verwaist" },
    columnPlace: "Wo",
    columnTag: "Tag",
    columnRequirement: "Anforderung",
    columnState: "Zustand",
    problem: { unknown: "nennt keine Anforderung", trashed: "im Papierkorb" },
    outdatedState: (revision: number, current: number) => `Veraltet: Revision ${revision} von ${current}`,
    orphanedState: "Verwaist",
    previous: "Zurück",
    next: "Weiter",
  },
  words: {
    title: "Code-Tag-Wörter",
    intro:
      "Die Wörter, mit denen ein Quellcode-Kommentar eine Anforderung taggt, etwa [implements KEY]. Tags werden nach diesen Wörtern gelesen, ein Wechsel braucht also keinen neuen Scan.",
    implements: "Wörter für implementiert",
    verifies: "Wörter für verifiziert",
    hint: "Trennen Sie Wörter mit Kommas.",
    save: "Speichern",
    saved: "Gespeichert.",
    readOnly: "Nur die Administratoren des Projekts legen die Tag-Wörter fest.",
  },
};
