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
    links: "Verknüpfungen",
    tests: "Testergebnisse",
    hint: "Was eine Anforderung dieses Typs verifiziert: verifizierende Verknüpfungen, bestandene Testergebnisse auf dem zählenden Branch, oder beides.",
  },
  matrix: {
    tests: "Tests",
    outdated: "veraltet",
  },
  chains: {
    tests: "Tests",
  },
};
