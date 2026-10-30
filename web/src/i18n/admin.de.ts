import type { adminEn } from "./admin.en";

/** The German strings of the Admin area, held to the English shape. */
export const adminDe: typeof adminEn = {
  title: "Admin",
  intro: "Alles, was die Administration für alle in dieser Installation festlegt.",
  navLabel: "Bereiche der Administration",
  menuLabel: "Bereich",
  allSections: "Alle Bereiche",
  inPalette: (page: string) => `Admin / ${page}`,
  refusedTitle: "Nur Administratoren können diese Seite öffnen",
  refusedBody: "Bitten Sie eine Administratorin oder einen Administrator dieser Installation um die Änderung oder um die Rolle.",
  sections: {
    people: "Personen und Zugriff",
    work: "Arbeit",
    integrations: "Integrationen",
    records: "Protokolle",
  },
  pages: {
    installation: { label: "Installation", detail: "Wie die Installation heißt, das Design, das alle sehen, bis sie eines wählen, und die Suite-Demo." },
    access: { label: "Zugriff", detail: "Rollen, was jede darf, die Mitglieder und Gruppen, die sie halten, und Single Sign-on." },
    users: { label: "Benutzer", detail: "Wer darauf wartet, eingelassen zu werden, Personen mit Passwort hinzufügen und Konten ab- oder wieder einschalten." },
    linkTypes: { label: "Verknüpfungsarten", detail: "Die Arten von Verknüpfungen zwischen Anforderungen, die jedes Projekt anbietet." },
    armature: { label: "Armature", detail: "Die Verbindung zu Armature: seine Adresse, seine App für Exciter und seine Ereignisse." },
    stator: { label: "Stator", detail: "Die Verbindung zu Stator: seine Adresse, seine App für Exciter und seine Ereignisse." },
    connectedApps: { label: "Verbundene Apps", detail: "Die Apps, die hier für Personen handeln dürfen, und jedes Token und jede Freigabe, die sie halten." },
    webhooks: { label: "Webhooks", detail: "Adressen, denen Exciter meldet, wenn sich Anforderungen, Verknüpfungen und Prüfungen ändern." },
    audit: { label: "Protokoll", detail: "Wer was an der Installation geändert hat, und wann." },
  },
  users: {
    title: "Benutzer",
    intro:
      "Lassen Sie ein, wer wartet, fügen Sie jemanden mit Passwort hinzu, und schalten Sie ein Konto ab, damit es sich nicht mehr anmelden kann, oder wieder ein. Rollen stehen unter Zugriff.",
    accounts: "Konten",
    columnState: "Zustand",
    on: "Kann sich anmelden",
    empty: "Noch ist niemand Mitglied.",
  },
  installation: {
    defaultTheme: "Standarddesign",
    defaultThemeIntro: "Was alle sehen, bis sie ein eigenes Design wählen. Jedes Design, das mit allen geteilt ist, kann es sein.",
    builtIn: "Das eingebaute Design",
    noShared: "Noch ist kein Design mit allen geteilt. Teilen Sie eines unter Designs, dann kann es der Standard sein.",
  },
  connectedApps: {
    title: "Verbundene Apps",
    intro:
      "Die Apps, die hier für Personen handeln dürfen: die Partnerwerkzeuge, die Assistenten, die Personen verbunden haben, und jedes Token, das Personen erstellt haben.",
  },
};
