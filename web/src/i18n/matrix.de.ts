import type { matrixEn } from "./matrix.en";

/** Die Texte der eigenen Seite der Verfolgbarkeitsmatrix (#147). */
export const matrixDe: typeof matrixEn = {
  title: "Verfolgbarkeitsmatrix",
  intro: "Anforderungen gegen das, womit sie verknüpft sind, gezählt nach den Verknüpfungen, die Sie wählen.",
  open: "Matrix öffnen",
  rows: "Zeilen",
  linkTypes: "Verknüpfungstypen",
  everyLinkType: "Solange keiner gewählt ist, zählt jeder Verknüpfungstyp.",
  direction: "Richtung",
  directions: { both: "Beide Richtungen", outgoing: "Ausgehend", incoming: "Eingehend" },
  columnProject: "Spalten aus",
  thisProject: "Diesem Projekt",
  relations: "Beziehungen",
  everyRelation: "Solange keine gewählt ist, zählt jede Beziehung.",
  relationNames: { implements: "Umgesetzt durch", verifies: "Geprüft durch", relates: "Verwandt mit" },
};
