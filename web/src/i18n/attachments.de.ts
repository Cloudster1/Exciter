import type { attachmentsEn } from "./attachments.en";

/** The German strings of a requirement's files, held to the English shape. */
export const attachmentsDe: typeof attachmentsEn = {
  title: "Dateien",
  count: (n: number) => (n === 1 ? "1 Datei" : `${n} Dateien`),
  loading: "Die Dateien werden geladen...",
  empty: "Es sind keine Dateien angehängt.",
  emptyEditable: "Es sind keine Dateien angehängt. Hängen Sie eine an, oder legen Sie sie hier ab.",
  attach: "Dateien anhängen",
  dropHere: "Legen Sie die Dateien ab, um sie anzuhängen.",
  uploading: (name: string) => `${name} wird hochgeladen...`,
  uploaded: (name: string) => `${name} angehängt.`,
  open: (name: string) => `${name} öffnen`,
  download: (name: string) => `${name} herunterladen`,
  remove: (name: string) => `${name} löschen`,
  confirmRemove: "Eine Revision, die die Datei zeigt, zeigt sie weiterhin.",
  removed: (name: string) => `${name} gelöscht.`,
  shown: "In der Aussage gezeigt",
  uploadedBy: (name: string, date: string) => (name ? `${name}, ${date}` : date),
  tooLarge: (name: string, size: string, why: string) => `${name} (${size}) wurde nicht angehängt. ${why}`,
  refused: (name: string, why: string) => `${name} wurde nicht angehängt. ${why}`,
  uploadBroke: (name: string) => `${name} wurde nicht angehängt, weil die Verbindung abbrach. Versuchen Sie es erneut.`,
  missingImage: "Dieses Bild ist nicht mehr gespeichert.",
  insert: "Hier eine Datei anhängen",
};
