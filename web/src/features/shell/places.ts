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
  ALL_CONNECTED_APPS_PATH,
  CONNECTED_APPS_PATH,
  PROFILE_PATH,
  PROJECTS_PATH,
  REVIEWS_PATH,
  SEARCH_PATH,
  SSO_PATH,
  STATOR_ACCOUNT_PATH,
  STATOR_SETTINGS_PATH,
  WEBHOOKS_PATH,
  THEMES_PATH,
  TOKENS_PATH,
  WATCHING_PATH,
} from "@/config";
import { t } from "@/i18n";

/** Where a place is listed: the sidebar's places, the settings area's You, or its Settings for administrators. */
export type PlaceArea = "work" | "you" | "settings";

/** A page of this build the reader can go to: the sidebar or the settings area lists it, the palette finds it, the
 *  breadcrumbs name it. */
export interface Place {
  path: string;
  icon: IconName;
  label: string;
  area: PlaceArea;
  /** Also in the avatar menu, among the person's own pages, by this name for the tests. */
  inAccountMenu?: string;
}

/** The settings area: its hub, and every page under it gets the settings sidebar in place of the main one. */
export const SETTINGS_PATH = "/settings";

// A function rather than a list, so the labels are read in the language shown now.
function allPlaces(): Place[] {
  return [
    { path: "/", icon: "Home", label: t.nav.home, area: "work" },
    { path: SEARCH_PATH, icon: "Search", label: t.nav.search, area: "work" },
    { path: NOTIFICATIONS_PATH, icon: "Bell", label: t.notifications.title, area: "work" },
    { path: REVIEWS_PATH, icon: "Check", label: t.review.inboxTitle, area: "work" },
    { path: PROJECTS_PATH, icon: "Folder", label: t.nav.projects, area: "work" },
    { path: PROFILE_PATH, icon: "User", label: t.account.profile, area: "you", inAccountMenu: "profile" },
    { path: NOTIFICATION_SETTINGS_PATH, icon: "Bell", label: t.account.notifications, area: "you", inAccountMenu: "notification-settings" },
    { path: WATCHING_PATH, icon: "Eye", label: t.account.watching, area: "you", inAccountMenu: "watching" },
    { path: THEMES_PATH, icon: "Palette", label: t.account.themes, area: "you", inAccountMenu: "themes" },
    { path: TOKENS_PATH, icon: "Key", label: t.account.tokens, area: "you", inAccountMenu: "tokens" },
    { path: CONNECTED_APPS_PATH, icon: "Link", label: t.account.connectedApps, area: "you", inAccountMenu: "connected-apps" },
    { path: ARMATURE_ACCOUNT_PATH, icon: "Link", label: t.account.armature, area: "you" },
    { path: STATOR_ACCOUNT_PATH, icon: "Link", label: t.account.stator, area: "you" },
    { path: INSTALLATION_PATH, icon: "Settings", label: t.nav.installation, area: "settings" },
    { path: MEMBERS_PATH, icon: "Users", label: t.nav.members, area: "settings" },
    { path: SSO_PATH, icon: "Lock", label: t.nav.sso, area: "settings" },
    { path: DEFAULT_THEME_PATH, icon: "Palette", label: t.nav.defaultTheme, area: "settings" },
    { path: LINK_TYPES_PATH, icon: "Link", label: t.nav.linkTypes, area: "settings" },
    { path: ALL_TOKENS_PATH, icon: "Key", label: t.nav.allTokens, area: "settings" },
    { path: ALL_CONNECTED_APPS_PATH, icon: "Link", label: t.nav.allConnectedApps, area: "settings" },
    { path: ARMATURE_SETTINGS_PATH, icon: "Link", label: t.nav.armature, area: "settings" },
    { path: STATOR_SETTINGS_PATH, icon: "Link", label: t.nav.stator, area: "settings" },
    { path: WEBHOOKS_PATH, icon: "Share", label: t.nav.webhooks, area: "settings" },
    { path: AUDIT_PATH, icon: "Shield", label: t.nav.audit, area: "settings" },
  ];
}

/** Every place the signed-in person may go, in the order the sidebar and the settings area list them; the
 *  installation's settings only for its administrators, as one check for the whole group. */
export function placesFor(me: Me | undefined): Place[] {
  const admin = administers(me?.role);
  return allPlaces().filter((place) => admin || place.area !== "settings");
}

/** The heading of each area's places, written before them in the palette. */
export function areaTitle(area: PlaceArea): string {
  if (area === "you") return t.nav.groupYou;
  if (area === "settings") return t.nav.settings;
  return t.nav.groupRequirements;
}

/** Whether a path is the settings area's, which draws its own sidebar. */
export function inSettings(pathname: string): boolean {
  return pathname === SETTINGS_PATH || pathname.startsWith(`${SETTINGS_PATH}/`);
}

/** One crumb of the trail: a place to go back to, or the page itself without a path. */
export interface PlaceCrumb {
  label: string;
  path?: string;
}

/** The crumbs from the installation to the page at a path, the page itself last; empty for a path that is no place.
 *  In the settings area every trail passes Settings, which leads to its hub. */
export function crumbsFor(pathname: string): PlaceCrumb[] {
  if (pathname === SETTINGS_PATH) return [{ label: t.nav.settings }];
  const place = allPlaces().find((each) => each.path === pathname);
  if (place) return inSettings(pathname) ? [{ label: t.nav.settings, path: SETTINGS_PATH }, { label: place.label }] : [{ label: place.label }];
  const above = placeAbove(pathname);
  if (!above) return [];
  return inSettings(pathname)
    ? [
        { label: t.nav.settings, path: SETTINGS_PATH },
        { label: above.label, path: above.path },
      ]
    : [{ label: above.label, path: above.path }];
}

/** Where a project's own pages begin; they all lie under the projects. */
export const PROJECT_PAGES_PREFIX = "/p/";

/** The place a page lies under, such as Themes for one theme's editor, when the page is no place itself. */
export function placeAbove(pathname: string): Place | undefined {
  const places = allPlaces();
  if (pathname.startsWith(PROJECT_PAGES_PREFIX)) return places.find((each) => each.path === PROJECTS_PATH);
  return places.find((each) => each.path !== "/" && pathname.startsWith(`${each.path}/`));
}
