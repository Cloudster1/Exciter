import type { NotificationKind } from "@/api/notifications";
import type { Watch, Watcher } from "@/api/watching";

/** The strings of watching requirements and projects (#34), after Stator's. */
export const watchEn = {
  button: "Watch",
  watching: "Watching",
  menu: (key: string) => `Watching ${key}`,
  requirement: "Watch this requirement",
  project: "Watch the whole project",
  stopRequirement: "Stop watching this requirement",
  stopProject: "Stop watching the project",
  coveredByProject: "Covered by your watch on the whole project",
  selected: "(on)",
  watchers: "Who is watching",
  watchersTitle: (key: string) => `Who is watching ${key}`,
  watchersIntro: "These people hear about new revisions, transitions and comments of this requirement.",
  watchersNone: "Nobody watches this requirement yet.",
  watchersMore: (shown: number, total: number) => `Showing ${shown} of ${total}.`,
  via: { requirement: "Watches this requirement", project: "Watches the whole project" } as Record<Watcher["via"], string>,
  failed: "Watching could not be changed. Try again in a moment.",
  projectButton: "Watch project",
  projectWatching: "Watching project",
  projectHint: (name: string) => `Hear about every requirement of ${name}, now and later.`,
  listTitle: "Watching",
  listIntro: "The requirements and projects you watch. You hear about their new revisions, transitions and comments.",
  listEmpty: "You watch nothing yet",
  listEmptyBody:
    "Open a requirement or a project and choose Watch. Requirements you write, revise or are made the owner of are watched for you unless you turn that off.",
  columnWhat: "Requirement or project",
  columnKind: "Covers",
  columnSince: "Since",
  columnActions: "Actions",
  kind: { requirement: "This requirement", project: "Every requirement of the project" } as Record<Watch["kind"], string>,
  projectNamed: (key: string) => `Project ${key}`,
  stop: "Stop watching",
  stopNamed: (what: string) => `Stop watching ${what}`,
  stopped: (what: string) => `You no longer watch ${what}.`,
  previous: "Previous",
  next: "Next",
  range: (from: number, to: number, total: number) => `${from} to ${to} of ${total}`,
};

/** The strings of the bell, the notifications page and the preferences (#34), after Stator's. */
export const notificationsEn = {
  bell: "Notifications",
  bellUnread: (n: number) => (n === 1 ? "Notifications, 1 unread" : `Notifications, ${n} unread`),
  title: "Notifications",
  markAllRead: "Mark all as read",
  empty: "Nothing new. You hear here about requirements you watch, and about comments that mention you.",
  emptyUnread: "You have read everything.",
  unread: "Unread",
  unreadOnly: "Unread only",
  failed: "Your notifications could not be loaded. Try again in a moment.",
  settings: "Notification settings",
  seeAll: "See all notifications",
  sentence: (kind: NotificationKind, actor: string, key: string): string => {
    const who = actor || "Somebody";
    switch (kind) {
      case "mentioned":
        return `${who} mentioned you in a comment on ${key}`;
      case "review_requested":
        return `${who} asks you to review ${key}`;
      case "reviewed":
        return `The review of ${key} is decided`;
      case "suspect":
        return `A link of ${key} needs checking after ${who} revised its other end`;
      case "replied":
        return `${who} replied in a thread on ${key}`;
      case "commented":
        return `${who} commented on ${key}`;
      case "resolved":
        return `${who} resolved or reopened a thread on ${key}`;
      case "revised":
        return `${who} saved a new revision of ${key}`;
      case "transitioned":
        return `${who} moved ${key} to another status`;
    }
  },
  preferencesTitle: "Notifications",
  preferencesIntro:
    "Choose what you hear about, in the app and by email. An email is a copy of what the app shows, so a kind switched off in the app sends no email either.",
  kindColumn: "When",
  inApp: "In the app",
  email: "By email",
  kinds: {
    mentioned: "Somebody mentions you in a comment",
    review_requested: "Somebody asks you to review a revision",
    reviewed: "A review you asked for, or of your revision, is decided",
    suspect: "A link of a requirement you own or watch needs checking",
    replied: "Somebody replies in a thread you wrote in",
    commented: "Somebody comments on a requirement you watch",
    resolved: "A thread you wrote in is resolved or reopened",
    revised: "A requirement you watch gets a new revision",
    transitioned: "A requirement you watch moves to another status",
  } as Record<NotificationKind, string>,
  inAppFor: (kind: string) => `In the app: ${kind}`,
  emailFor: (kind: string) => `By email: ${kind}`,
  digest: "When emails go out",
  digests: {
    off: "One email for each notification, at once",
    hourly: "One email an hour with everything new",
    daily: (hour: number) => `One email a day at ${String(hour).padStart(2, "0")}:00 UTC with everything new`,
  },
  digestHint: "A bundle leaves out what you have already read in the app.",
  autoWatch: "Watch the requirements I write, revise or am made the owner of",
  autoWatchHint: "Stopping watching a requirement keeps you from watching it again through your own edits.",
  save: "Save",
  saved: "Your notification settings are saved.",
  previous: "Previous",
  next: "Next",
  range: (from: number, to: number, total: number) => `${from} to ${to} of ${total}`,
};
