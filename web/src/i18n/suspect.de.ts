import type { suspectEn } from "./suspect.en";

/** Die Texte zu verdächtigen Verknüpfungen (#36). */
export const suspectDe: typeof suspectEn = {
  badge: "Verdächtig",
  badgeHint: "Eine Anforderung an einem Ende hat sich geändert, seit diese Verknüpfung zuletzt bestätigt wurde.",
  count: (n) => (n === 1 ? "1 verdächtige Verknüpfung" : `${n} verdächtige Verknüpfungen`),
  onlySuspect: "Nur verdächtige Verknüpfungen",
  check: "Prüfen",
  checkLabel: (key) => `Die Verknüpfung zu ${key} prüfen`,
  dialogTitle: (from, phrase, to) => `Prüfen: ${from} ${phrase} ${to}`,
  intro:
    "Eine Anforderung an einem Ende hat sich geändert, seit diese Verknüpfung zuletzt bestätigt wurde. Lesen Sie die Änderung und bestätigen Sie dann, dass die Verknüpfung noch gilt.",
  moved: (key, from, to) => `${key}, Revision ${from} bis ${to}`,
  comment: "Warum die Verknüpfung noch gilt",
  confirm: "Verknüpfung bestätigen",
  cancel: "Abbrechen",
  cleared: (key) => `Die Verknüpfung zu ${key} ist bestätigt.`,
  earlier: "Früher bestätigt",
  earlierRow: (who, when, source, target) => `${who || "Ein früheres Mitglied"}, ${when}, bei den Revisionen ${source} und ${target}`,
  listTitle: "Verdächtige Verknüpfungen",
  listIntro: "Verknüpfungen, deren Anforderung an einem Ende sich seit der letzten Bestätigung geändert hat, die am längsten wartenden zuerst.",
  none: "Keine Verknüpfung wartet auf eine Prüfung.",
  since: (when) => `Wartet seit ${when}`,
  previous: "Zurück",
  next: "Weiter",
  range: (first, last, total) => `${first} bis ${last} von ${total}`,
  armatureTitle: (key, issue) => `Die Verfolgung von ${key} zu ${issue} prüfen`,
  armatureIntro:
    "Diese Anforderung hat sich geändert, seit ihre Verfolgung zum Vorgang zuletzt bestätigt wurde. Lesen Sie die Änderung und bestätigen Sie dann, dass der Vorgang sie noch abdeckt.",
  armatureLabel: (issue) => `Die Verfolgung zu ${issue} prüfen`,
  armatureCleared: (issue) => `Die Verfolgung zu ${issue} ist bestätigt.`,
  statorHint: "Die Seite hat eine neuere Version als die, gegen die diese Verknüpfung zuletzt bestätigt wurde.",
  statorTitle: (key, page) => `Die Verfolgung von ${key} zu ${page} prüfen`,
  statorIntro: (from, to) =>
    `Die Seite ist von Version ${from} auf Version ${to} gegangen, seit ihre Verfolgung zuletzt bestätigt wurde. Lesen Sie sie in Stator und bestätigen Sie dann, dass die Anforderung ihr noch folgt.`,
  statorOpen: "Die Seite in Stator öffnen",
  statorLabel: (page) => `Die Verfolgung zu ${page} prüfen`,
  statorCleared: (page) => `Die Verfolgung zu ${page} ist bestätigt.`,
};
