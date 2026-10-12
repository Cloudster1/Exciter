import type { treeEn } from "./tree.en";

/** The German strings of the tree and the specification, held to the English shape. */
export const treeDe: typeof treeEn = {
  specTitle: "Spezifikation",
  navLabel: "Anforderungsbaum",
  label: (project: string) => `Anforderungen von ${project}`,
  keys: "Pfeiltasten durchlaufen den Baum, Enter öffnet eine Anforderung und M verschiebt sie.",
  empty: "Noch ist nichts geschrieben.",
  moved: (key: string, outline: string) => `${key} nach ${outline} verschoben.`,
  preview: (key: string, outline: string) => `${key} würde ${outline}.`,
  more: "Mehr zeigen",
  move: "Verschieben",
  copy: "Kopieren",
  moveTitle: (key: string) => `${key} verschieben`,
  copyTitle: (key: string) => `${key} kopieren`,
  project: "Projekt",
  parent: "Unter",
  parentHint: "Sie kommt als Letzte unter die gewählte Anforderung oder den gewählten Abschnitt.",
  top: "Die oberste Ebene des Projekts",
  loadingPlaces: "Baum wird geladen...",
  moveHint:
    "Alles darunter wandert mit. In einem anderen Projekt bekommt jede verschobene Anforderung einen neuen Schlüssel, und der alte funktioniert weiter.",
  copyChildren: "Auch alles darunter kopieren",
  moveSubmit: "Verschieben",
  copySubmit: "Kopieren",
  copiedFrom: (key: string) => `Kopiert von ${key}`,
  addBelow: "Darunter hinzufügen",
};
