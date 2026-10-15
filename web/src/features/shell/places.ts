import { administers, type Me } from "@/api/auth";
import type { IconName } from "@/components/icons";
import {
  ARMATURE_ACCOUNT_PATH,
  ARMATURE_SETTINGS_PATH,
  AUDIT_PATH,
  DEFAULT_THEME_PATH,
  INSTALLATION_PATH,
  LINK_TYPES_PATH,
  MEMBERS_PATH,
  NOTIFICATION_SETTINGS_PATH,
  NOTIFICATIONS_PATH,
  ALL_TOKENS_PATH,
  PROJECTS_PATH,
  REVIEWS_PATH,
  SEARCH_PATH,
  SSO_PATH,
  STATOR_ACCOUNT_PATH,
  STATOR_SETTINGS_PATH,
  THEMES_PATH,
  TOKENS_PATH,
  WATCHING_PATH,
} from "@/config";
import { t } from "@/i18n";

/** A page of this build the reader can go to: the sidebar lists it, the quick switcher finds it, the breadcrumbs name it. */
export interface Place {
  path: string;
  icon: IconName;
  label: string;
  /** The group it sits in, written before it in the breadcrumbs. */
  section?: string;
  /** Kept for the installation's owners and administrators. */
  adminOnly?: boolean;
  /** The person's own, reached from the account menu rather than the sidebar. */
  personal?: boolean;
}

/** Where the settings rail entry leads; the router sends it on to the first settings page. */
export const SETTINGS_PATH = "/settings";

// A function rather than a list, so the labels are read in the language shown now.
function allPlaces(): Place[] {
  return [
    { path: "/", icon: "Home", label: t.nav.home },
    { path: PROJECTS_PATH, icon: "Folder", label: t.nav.projects },
    { path: SEARCH_PATH, icon: "Search", label: t.nav.search },
    { path: REVIEWS_PATH, icon: "Check", label: t.review.inboxTitle },
    { path: MEMBERS_PATH, icon: "Users", label: t.nav.members, section: t.nav.settings, adminOnly: true },
    { path: SSO_PATH, icon: "Lock", label: t.nav.sso, section: t.nav.settings, adminOnly: true },
    { path: DEFAULT_THEME_PATH, icon: "Palette", label: t.nav.defaultTheme, section: t.nav.settings, adminOnly: true },
    { path: LINK_TYPES_PATH, icon: "Link", label: t.nav.linkTypes, section: t.nav.settings, adminOnly: true },
    { path: ALL_TOKENS_PATH, icon: "Key", label: t.nav.allTokens, section: t.nav.settings, adminOnly: true },
    { path: ARMATURE_SETTINGS_PATH, icon: "Link", label: t.nav.armature, section: t.nav.settings, adminOnly: true },
    { path: STATOR_SETTINGS_PATH, icon: "Link", label: t.nav.stator, section: t.nav.settings, adminOnly: true },
    { path: AUDIT_PATH, icon: "Shield", label: t.nav.audit, section: t.nav.settings, adminOnly: true },
    { path: INSTALLATION_PATH, icon: "Settings", label: t.nav.installation, section: t.nav.settings, adminOnly: true },
    { path: THEMES_PATH, icon: "Palette", label: t.nav.themes, section: t.account.menu, personal: true },
    { path: TOKENS_PATH, icon: "Key", label: t.account.tokens, section: t.account.menu, personal: true },
    { path: ARMATURE_ACCOUNT_PATH, icon: "Link", label: t.account.armature, section: t.account.menu, personal: true },
    { path: STATOR_ACCOUNT_PATH, icon: "Link", label: t.account.stator, section: t.account.menu, personal: true },
    { path: NOTIFICATIONS_PATH, icon: "Bell", label: t.notifications.title, section: t.account.menu, personal: true },
    { path: NOTIFICATION_SETTINGS_PATH, icon: "Settings", label: t.account.notifications, section: t.account.menu, personal: true },
    { path: WATCHING_PATH, icon: "Eye", label: t.account.watching, section: t.account.menu, personal: true },
  ];
}

/** Every place the signed-in person may go, in the order the sidebar lists them. */
export function placesFor(me: Me | undefined): Place[] {
  const admin = administers(me?.role);
  return allPlaces().filter((place) => admin || !place.adminOnly);
}

/** The crumbs from the installation to the page at a path, the page itself last; empty for a path that is no place. */
export function crumbsFor(pathname: string): string[] {
  const place = allPlaces().find((each) => each.path === pathname);
  if (!place) return [];
  return place.section ? [place.section, place.label] : [place.label];
}

/** Where a project's own pages begin; they all lie under the projects. */
export const PROJECT_PAGES_PREFIX = "/p/";

/** The place a page lies under, such as Themes for one theme's editor, when the page is no place itself. */
export function placeAbove(pathname: string): Place | undefined {
  const places = allPlaces();
  if (pathname.startsWith(PROJECT_PAGES_PREFIX)) return places.find((each) => each.path === PROJECTS_PATH);
  return places.find((each) => each.path !== "/" && pathname.startsWith(`${each.path}/`));
}
