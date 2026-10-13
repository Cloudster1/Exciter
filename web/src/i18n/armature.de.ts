import type { armatureEn } from "./armature.en";

/** The German strings of the Armature connection and account, held to the English shape. */
export const armatureDe: typeof armatureEn = {
  title: "Armature",
  intro:
    "Verbinden Sie die Armature-Instanz, in der Ihre Organisation ihre Arbeit verfolgt. Anforderungen lassen sich dann auf ihre Vorgänge zurückverfolgen, und jede Person sieht, was Armature ihr zeigt.",
  notAdmin: "Nur ein Administrator dieser Organisation kann Armature verbinden.",
  baseUrl: "Armature-Adresse",
  baseUrlHint: "Wo Armature geöffnet wird, etwa https://armature.example.com.",
  orgSlug: "Armature-Organisation",
  orgSlugHint: "Das Kürzel Ihrer Organisation in Armature. Das Token jedes Mitglieds muss zu ihr gehören.",
  movesForget:
    "Wenn Sie die Adresse oder die Organisation ändern, werden die Tokens aller Mitglieder verworfen. Sie verbinden sich dann unter Ihr Konto, Armature-Konto neu.",
  connect: "Armature verbinden",
  save: "Speichern",
  saved: "Gespeichert.",
  connectedCount: (count: number) => (count === 1 ? "1 Mitglied hat sein Konto verbunden." : `${count} Mitglieder haben ihr Konto verbunden.`),
  orgKnown: "Armature hat die Organisation über das Token eines Mitglieds bestätigt.",
  webhookTitle: "Webhook",
  webhookIntro: "Armature meldet Exciter, wenn sich Vorgänge ändern, damit eine Anforderung die Änderung sofort zeigt statt innerhalb einer Minute.",
  webhookSteps: [
    "Öffnen Sie in Armature Settings, dann Webhooks.",
    "Legen Sie einen Endpunkt mit der Adresse unten an und abonnieren Sie die aufgeführten Themen.",
    "Fügen Sie das Geheimnis, das Armature einmal anzeigt, in das Feld oben ein und speichern Sie.",
  ],
  webhookUrl: "Webhook-Adresse",
  topics: "Themen",
  copy: "Adresse kopieren",
  copied: "Adresse kopiert.",
  copyFailed: "Das Kopieren hat nicht geklappt. Markieren Sie die Adresse und kopieren Sie sie selbst.",
  webhookSecret: "Webhook-Geheimnis",
  secretStored: "Gespeichert. Leer lassen, um es zu behalten.",
  forgetSecret: "Gespeichertes Geheimnis verwerfen",
  disconnectTitle: "Trennen",
  disconnectIntro:
    "Beim Trennen werden die Adresse, das Webhook-Geheimnis und die Tokens aller Mitglieder verworfen. Anforderungen zeigen dann nur noch Vorgangsschlüssel.",
  disconnect: "Armature trennen",
  confirmDisconnect: "Armature trennen? Die Tokens aller Mitglieder werden verworfen, und Anforderungen zeigen nur noch Vorgangsschlüssel.",
  disconnected: "Armature ist getrennt.",
  account: {
    title: "Armature-Konto",
    intro: "Verbinden Sie Ihr Armature-Konto, damit Anforderungen Ihnen die Vorgänge zeigen, die Sie in Armature sehen dürfen.",
    notConfigured: "Ihre Organisation hat Armature noch nicht verbunden. Bitten Sie einen Administrator, es unter Einstellungen, Armature zu verbinden.",
    steps:
      "Öffnen Sie in Armature Tokens und erstellen Sie ein Token ohne den Bereich read, damit Exciter für Sie auch Vorgänge anlegen und verknüpfen kann. Fügen Sie es dann hier ein.",
    openArmature: "Armature öffnen",
    token: "Armature-Token",
    connect: "Verbinden",
    replace: "Neues Token einfügen",
    connectedAs: (name: string, email: string) => `Verbunden als ${name} (${email}).`,
    checkedAt: (when: string) => `Zuletzt geprüft ${when}.`,
    readOnly:
      "Dieses Token darf nur lesen. Exciter zeigt Ihnen damit Vorgänge, kann aber keine Vorgänge anlegen und keine Anforderungen mit ihnen verknüpfen. Fügen Sie dafür ein Token ohne den Bereich read ein.",
    check: "Erneut prüfen",
    checked: "Armature akzeptiert Ihr Token.",
    disconnect: "Trennen",
    confirmDisconnect: "Ihr Armature-Konto trennen? Anforderungen zeigen nur noch Vorgangsschlüssel, bis Sie es wieder verbinden.",
    disconnected: "Ihr Armature-Konto ist getrennt.",
    rejected: "Armature akzeptiert Ihr Token nicht mehr. Erstellen Sie in Armature unter Tokens ein neues und fügen Sie es hier ein.",
    unreachable: "Armature hat auf die letzte Prüfung nicht geantwortet. Prüfen Sie gleich noch einmal.",
  },
};
