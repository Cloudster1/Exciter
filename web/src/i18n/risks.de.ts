import type { Dimension } from "@/api/risks";
import type { risksEn } from "./risks.en";

const dimensions: Record<Dimension, string> = { severity: "Schwere", probability: "Wahrscheinlichkeit", detection: "Entdeckung" };

/** Die Texte der Risiken: Anforderungen eines als Risiken markierten Typs, bewertet nach dem Risikoschema des Projekts (#259). */
export const risksDe: typeof risksEn = {
  roleLabel: "Seine Anforderungen sind",
  roles: { requirement: "Anforderungen", testCase: "Testfälle", risk: "Risiken" },
  roleHints: {
    requirement: "Anforderungen, wie die meisten Typen sie haben.",
    testCase: "Ein Testfall trägt zusätzlich Vorbedingungen, Schritte mit je einem erwarteten Ergebnis und Parameter.",
    risk: "Ein Risiko wird vor und nach der Minderung nach dem Risikoschema des Projekts bewertet.",
  },
  roleHeld:
    "Einige seiner Anforderungen haben Schritte oder eine Bewertung, die nur diese Rolle gibt; daher behält er seine Rolle. Entfernen Sie diese oder geben Sie den Anforderungen zuerst einen anderen Typ derselben Rolle.",
  typeTag: "Risiken",
  tab: "Risiken",
  schemeTitle: "Risikoschema",
  schemeIntro:
    "Die Skalen, auf denen ein Risiko bewertet wird, die Stufen, die eine Bewertung ergibt, und wie sie sich ergeben. Eine Stufe wird bei jedem Lesen ermittelt; eine Änderung hier verschiebt also jedes Risiko sofort.",
  noScheme:
    "Noch kein Risikoschema, daher kann kein Risiko bewertet werden. Beginnen Sie mit einer Fünf-mal-fünf-Matrix und ändern Sie, was dieses Projekt braucht.",
  startFromDefault: "Mit fünf mal fünf beginnen",
  readOnly: "Nur die Administratoren des Projekts legen sein Risikoschema fest.",
  method: "Eine Bewertung ergibt ihre Stufe durch",
  methods: { matrix: "Eine Matrix aus Schwere und Wahrscheinlichkeit", rpn: "Eine Risikoprioritätszahl: Schwere mal Wahrscheinlichkeit mal Entdeckung" },
  detection: "Auch die Entdeckung bewerten",
  detectionHint: "Wie schwer ein Fehler zu finden ist, bevor er schadet; ein höherer Wert ist schwerer.",
  dimensions,
  scaleLabel: (dimension: Dimension) => `Wie dieses Projekt ${dimensions[dimension]} nennt`,
  valueName: (dimension: Dimension, n: number) => `${dimensions[dimension]} ${n}`,
  addValue: (dimension: Dimension) => `Einen Wert zu ${dimensions[dimension]} hinzufügen`,
  removeValue: (dimension: Dimension) => `Den höchsten Wert von ${dimensions[dimension]} entfernen`,
  valueHeld: "Ein Risiko ist mit diesem Wert bewertet, daher bleibt er.",
  levels: "Stufen",
  levelsHint: "Von der geringsten zur schwersten. Ein Risiko auf einer akzeptablen Stufe braucht keine Minderung.",
  levelName: (n: number) => `Name der Stufe ${n}`,
  levelColour: (n: number) => `Farbe der Stufe ${n}`,
  acceptable: (name: string) => `${name || "Diese Stufe"} ist akzeptabel`,
  minRpn: (name: string) => `${name || "Diese Stufe"} ab Risikoprioritätszahl`,
  addLevel: "Eine Stufe hinzufügen",
  removeLevel: (name: string) => `Die Stufe ${name || "ohne Namen"} entfernen`,
  matrix: "Matrix",
  matrixHint: "Wählen Sie die Stufe jeder Zelle: Schwere nach rechts, Wahrscheinlichkeit nach unten.",
  cell: (severity: number, probability: number) => `Stufe bei Schwere ${severity} und Wahrscheinlichkeit ${probability}`,
  save: "Schema speichern",
  saved: "Das Risikoschema ist gespeichert.",
  rating: "Risikobewertung",
  ratingHint: "Bewerten Sie das Risiko vor der Minderung und so, wie es sein wird, sobald seine Minderungen wirken.",
  sides: { before: "Vor der Minderung", after: "Nach der Minderung" },
  value: (dimension: Dimension, side: "before" | "after") => `${dimensions[dimension]} ${side === "before" ? "vor" : "nach"} der Minderung`,
  unknown: "Nicht bewertet",
  rpn: "Risikoprioritätszahl",
  level: "Stufe",
  noLevel: "Noch keine Stufe",
  needsScheme:
    "Dieses Projekt hat noch kein Risikoschema, daher können seine Risiken nicht bewertet werden. Bitten Sie einen Administrator, eines unter Einstellungen, Risiken festzulegen.",
  dropped: "Sein neuer Typ ist keiner für Risiken; beim Speichern entfällt daher seine Bewertung.",
  diffTitle: "Risikobewertung",
  diff: (from: number | null, to: number | null) => `${from ?? "nicht bewertet"} zu ${to ?? "nicht bewertet"}`,
};
