import type { accessEn, permissionsEn } from "./permissions.en";

/** The German strings of a project's permissions, held to the English shape. */
export const permissionsDe: typeof permissionsEn = {
  title: "Berechtigungen",
  navLabel: "Berechtigungen",
  intro:
    "Wer in diesem Projekt Anforderungen lesen, ändern und freigeben darf. Rollen addieren sich: Jeder hat alles, was seine eigenen Freigaben, die seiner Gruppen und die für alle zusammen geben.",
  notProjectAdmin: "Nur die Administratoren des Projekts sehen und ändern, wer darin was darf.",
  archived: "Dieses Projekt ist archiviert, daher bleiben seine Berechtigungen, wie sie sind, bis es wiederhergestellt wird.",
  empty: "Niemand außer den Eigentümern und Administratoren der Organisation darf dieses Projekt öffnen.",
  tableLabel: "Wer welche Rolle hat",
  columnWho: "Wer",
  columnRole: "Rolle",
  columnRemove: "Entfernen",
  roles: { viewer: "Leser", reviewer: "Prüfer", editor: "Bearbeiter", admin: "Admin" },
  roleHints: {
    viewer: "Liest das Projekt, seine Anforderungen, deren Verlauf, Verknüpfungen und Dateien und exportiert sie.",
    reviewer: "Liest, kommentiert und gibt frei, was zur Prüfung ansteht.",
    editor: "Liest, kommentiert und schreibt, ändert, verknüpft und verschiebt Anforderungen.",
    admin: "Alles, dazu die Einstellungen, Berechtigungen, den Papierkorb und das Löschen des Projekts.",
  },
  roleOf: (name: string) => `Rolle von ${name}`,
  remove: (name: string) => `${name} entfernen`,
  yourself: (name: string) => `${name} (Sie)`,
  everyone: "Alle",
  everyoneDetail: "Jedes Mitglied der Organisation, außer Gästen",
  members: (n: number) => (n === 1 ? "1 Mitglied" : `${n} Mitglieder`),
  pickerLabel: "Person, Gruppe oder alle hinzufügen",
  pickerPlaceholder: "Name oder E-Mail eingeben",
  pickerOptions: "Personen und Gruppen",
  pickerLoading: "Suche...",
  pickerEmpty: "Niemand mit diesem Namen.",
  added: (name: string) => `${name} als Leser hinzugefügt. Wählen Sie eine Rolle, und speichern Sie dann.`,
  save: "Berechtigungen speichern",
  discard: "Änderungen verwerfen",
  saved: "Berechtigungen gespeichert.",
  unsaved: "Noch nicht gespeichert.",
  orgAdminsNote: "Die Eigentümer und Administratoren der Organisation sind in jedem Projekt Admin, egal was diese Tabelle sagt.",
  checkAccess: "Zugriff einer Person prüfen",
};

/** The German strings of the access inspector, held to the English shape. */
export const accessDe: typeof accessEn = {
  title: (project: string) => `Zugriff auf ${project}`,
  intro: "Wählen Sie ein Mitglied, um zu sehen, was es in diesem Projekt darf und was jedes Recht entscheidet.",
  pickerLabel: "Mitglied",
  pickerPlaceholder: "Name oder E-Mail eingeben",
  pickerOptions: "Mitglieder",
  loading: "Zugriff wird geprüft",
  heading: (name: string) => `Was ${name} darf`,
  rights: { view: "Lesen", comment: "Kommentieren", edit: "Bearbeiten", approve: "Freigeben", administer: "Verwalten" },
  allowed: "Erlaubt",
  denied: "Nicht erlaubt",
  passed: "Erfüllt",
  failed: "Nicht erfüllt",
  decides: "Das entscheidet es.",
  orgAdmin: (owner: boolean) =>
    owner
      ? "Ist Eigentümer der Organisation und hat damit jedes Recht in jedem Projekt."
      : "Verwaltet die Organisation und hat damit jedes Recht in jedem Projekt.",
  fromProvider: "Die Gruppen des Identitätsanbieters geben diese Rolle.",
  memberReached: "Ist Mitglied der Organisation, daher erreicht die Freigabe für alle auch diese Person.",
  guestNamed: "Ist Gast und wird nur von Freigaben erreicht, die die Person oder eine ihrer Gruppen nennen.",
  guestNotReached: "Ist Gast, daher erreicht die Freigabe für alle diese Person nicht. Nennen Sie die Person oder eine ihrer Gruppen, um sie hereinzulassen.",
  grantYes: "Hat hier eine Rolle, die dieses Recht gibt:",
  grantNo: "Keine Freigabe, die diese Person erreicht, gibt dieses Recht.",
  archived: "Das Projekt ist archiviert, daher ändert sich darin nichts, bis es wiederhergestellt wird.",
};
