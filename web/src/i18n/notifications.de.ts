import type { notificationsEn, watchEn } from "./notifications.en";

/** Die Texte zum Beobachten von Anforderungen und Projekten (#34). */
export const watchDe: typeof watchEn = {
  button: "Beobachten",
  watching: "Beobachtet",
  menu: (key: string) => `${key} beobachten`,
  requirement: "Diese Anforderung beobachten",
  project: "Das ganze Projekt beobachten",
  stopRequirement: "Diese Anforderung nicht mehr beobachten",
  stopProject: "Das Projekt nicht mehr beobachten",
  coveredByProject: "Erfasst, weil Sie das ganze Projekt beobachten",
  selected: "(an)",
  watchers: "Wer beobachtet",
  watchersTitle: (key: string) => `Wer ${key} beobachtet`,
  watchersIntro: "Diese Personen erfahren von neuen Revisionen, Übergängen und Kommentaren dieser Anforderung.",
  watchersNone: "Noch beobachtet niemand diese Anforderung.",
  watchersMore: (shown: number, total: number) => `${shown} von ${total} angezeigt.`,
  via: { requirement: "Beobachtet diese Anforderung", project: "Beobachtet das ganze Projekt" },
  failed: "Das Beobachten konnte nicht geändert werden. Versuchen Sie es gleich noch einmal.",
  projectButton: "Projekt beobachten",
  projectWatching: "Projekt beobachtet",
  projectHint: (name: string) => `Von jeder Anforderung in ${name} erfahren, jetzt und später.`,
  listTitle: "Beobachtet",
  listIntro: "Die Anforderungen und Projekte, die Sie beobachten. Sie erfahren von neuen Revisionen, Übergängen und Kommentaren.",
  listEmpty: "Sie beobachten noch nichts",
  listEmptyBody:
    "Öffnen Sie eine Anforderung oder ein Projekt und wählen Sie Beobachten. Anforderungen, die Sie schreiben, überarbeiten oder übernehmen, werden für Sie beobachtet, sofern Sie das nicht ausschalten.",
  columnWhat: "Anforderung oder Projekt",
  columnKind: "Umfasst",
  columnSince: "Seit",
  columnActions: "Aktionen",
  kind: { requirement: "Diese Anforderung", project: "Jede Anforderung des Projekts" },
  projectNamed: (key: string) => `Projekt ${key}`,
  stop: "Nicht mehr beobachten",
  stopNamed: (what: string) => `${what} nicht mehr beobachten`,
  stopped: (what: string) => `Sie beobachten ${what} nicht mehr.`,
  previous: "Zurück",
  next: "Weiter",
  range: (from: number, to: number, total: number) => `${from} bis ${to} von ${total}`,
};

/** Die Texte der Glocke, der Benachrichtigungen und ihrer Einstellungen (#34). */
export const notificationsDe: typeof notificationsEn = {
  bell: "Benachrichtigungen",
  bellUnread: (n: number) => (n === 1 ? "Benachrichtigungen, 1 ungelesen" : `Benachrichtigungen, ${n} ungelesen`),
  title: "Benachrichtigungen",
  markAllRead: "Alle als gelesen markieren",
  empty: "Nichts Neues. Hier erfahren Sie von Anforderungen, die Sie beobachten, und von Kommentaren, die Sie erwähnen.",
  emptyUnread: "Sie haben alles gelesen.",
  unread: "Ungelesen",
  unreadOnly: "Nur ungelesene",
  failed: "Ihre Benachrichtigungen konnten nicht geladen werden. Versuchen Sie es gleich noch einmal.",
  settings: "Benachrichtigungseinstellungen",
  seeAll: "Alle Benachrichtigungen",
  sentence: (kind, actor, key) => {
    const who = actor || "Jemand";
    switch (kind) {
      case "mentioned":
        return `${who} hat Sie in einem Kommentar zu ${key} erwähnt`;
      case "review_requested":
        return `${who} bittet Sie, ${key} zu prüfen`;
      case "reviewed":
        return `${who} hat über die Prüfung von ${key} entschieden`;
      case "suspect":
        return `Eine Verknüpfung von ${key} muss geprüft werden, weil ${who} ihr anderes Ende überarbeitet hat`;
      case "source_revised":
        return `Die Bibliotheksquelle von ${key} hat eine neue Revision von ${who}`;
      case "replied":
        return `${who} hat in einer Diskussion zu ${key} geantwortet`;
      case "commented":
        return `${who} hat ${key} kommentiert`;
      case "resolved":
        return `${who} hat eine Diskussion zu ${key} erledigt oder wieder geöffnet`;
      case "revised":
        return `${who} hat eine neue Revision von ${key} gespeichert`;
      case "transitioned":
        return `${who} hat ${key} in einen anderen Status gebracht`;
      case "session_invited":
        return `${who} bittet Sie, an der Review-Sitzung ${key} teilzunehmen`;
      case "session_round":
        return `${who} hat eine neue Runde der Review-Sitzung ${key} begonnen`;
      case "session_reminder":
        return `Die Review-Sitzung ${key} endet bald und wartet auf Sie`;
      case "session_closed":
        return `${who} hat die Review-Sitzung ${key} geschlossen`;
      case "code_outdated":
        return `Ein Scan von ${key} bringt Code-Tags, die ältere Revisionen von Anforderungen nennen, denen Sie folgen`;
      case "code_orphaned":
        return `Ein Scan von ${key} bringt Code-Tags, die Anforderungen im Papierkorb oder gar keine nennen`;
      case "term_changed":
        return `Ein Begriff, den ${key} verwendet, hat eine neue Definition von ${who}`;
      case "execution_assigned":
        return `${who} bittet Sie, in der Testdurchführung ${key} zu testen`;
      case "execution_reminder":
        return `Die Testdurchführung ${key} ist bald fällig`;
      case "execution_closed":
        return `${who} hat die Testdurchführung ${key} abgeschlossen`;
    }
  },
  preferencesTitle: "Benachrichtigungen",
  preferencesIntro:
    "Wählen Sie, wovon Sie erfahren, in der App und per E-Mail. Eine E-Mail ist eine Kopie dessen, was die App zeigt; eine Art, die in der App ausgeschaltet ist, verschickt also auch keine E-Mail.",
  kindColumn: "Wenn",
  inApp: "In der App",
  email: "Per E-Mail",
  kinds: {
    mentioned: "Jemand erwähnt Sie in einem Kommentar",
    review_requested: "Jemand bittet Sie, eine Revision zu prüfen",
    reviewed: "Über eine Prüfung, um die Sie gebeten haben oder die Ihre Revision betrifft, ist entschieden",
    suspect: "Eine Verknüpfung einer Anforderung, die Ihnen gehört oder die Sie beobachten, muss geprüft werden",
    source_revised: "Die Bibliotheksquelle einer Anforderung, die Sie beobachten, bekommt eine neue Revision",
    replied: "Jemand antwortet in einer Diskussion, in der Sie geschrieben haben",
    commented: "Jemand kommentiert eine Anforderung, die Sie beobachten",
    resolved: "Eine Diskussion, in der Sie geschrieben haben, wird erledigt oder wieder geöffnet",
    revised: "Eine Anforderung, die Sie beobachten, bekommt eine neue Revision",
    transitioned: "Eine Anforderung, die Sie beobachten, wechselt den Status",
    session_invited: "Jemand bittet Sie, an einer Review-Sitzung teilzunehmen",
    session_round: "Eine Review-Sitzung, an der Sie teilnehmen, beginnt eine neue Runde",
    session_reminder: "Eine Review-Sitzung, die auf Sie wartet, endet bald",
    session_closed: "Eine Review-Sitzung, an der Sie teilnehmen, ist geschlossen",
    code_outdated: "Ein Code-Scan bringt Tags, die eine ältere Revision einer Anforderung nennen, die Ihnen gehört oder die Sie beobachten",
    code_orphaned: "Ein Code-Scan bringt Tags, die eine Anforderung im Papierkorb nennen, oder keine in einem Projekt, das Sie verwalten",
    term_changed: "Die Definition eines Glossarbegriffs ändert sich, den eine Ihrer Anforderungen verwendet",
    execution_assigned: "Jemand bittet Sie, in einer Testdurchführung zu testen",
    execution_reminder: "Eine Testdurchführung, in der Sie testen, ist bald fällig",
    execution_closed: "Eine Testdurchführung, in der Sie testen oder die Sie verantworten, ist abgeschlossen",
  },
  inAppFor: (kind: string) => `In der App: ${kind}`,
  emailFor: (kind: string) => `Per E-Mail: ${kind}`,
  digest: "Wann E-Mails verschickt werden",
  digests: {
    off: "Eine E-Mail je Benachrichtigung, sofort",
    hourly: "Eine E-Mail pro Stunde mit allem Neuen",
    daily: (hour: number) => `Eine E-Mail am Tag um ${String(hour).padStart(2, "0")}:00 UTC mit allem Neuen`,
  },
  digestHint: "Eine Sammel-E-Mail lässt weg, was Sie in der App schon gelesen haben.",
  autoWatch: "Anforderungen beobachten, die ich schreibe, überarbeite oder übernehme",
  autoWatchHint: "Wer eine Anforderung nicht mehr beobachtet, beobachtet sie durch eigene Änderungen nicht wieder.",
  save: "Speichern",
  saved: "Ihre Benachrichtigungseinstellungen sind gespeichert.",
  previous: "Zurück",
  next: "Weiter",
  range: (from: number, to: number, total: number) => `${from} bis ${to} von ${total}`,
};
