import type { Me } from "@/api/auth";
import type { IconName } from "@/components/icons";
import {
  ACCESS_PATH,
  ADMIN_PATH,
  ARMATURE_ACCOUNT_PATH,
  ARMATURE_SETTINGS_PATH,
  AUDIT_PATH,
  INSTALLATION_PATH,
  LINK_TYPES_PATH,
  NOTIFICATION_SETTINGS_PATH,
  NOTIFICATIONS_PATH,
  ALL_CONNECTED_APPS_PATH,
  CONNECTED_APPS_PATH,
  PROFILE_PATH,
  PROJECTS_PATH,
  REVIEWS_PATH,
  SEARCH_PATH,
  STATOR_ACCOUNT_PATH,
  STATOR_SETTINGS_PATH,
  USERS_PATH,
  WEBHOOKS_PATH,
  THEMES_PATH,
  TOKENS_PATH,
  WATCHING_PATH,
} from "@/config";
import { t } from "@/i18n";
import { projectSettingsPath, sectionOfPath } from "@/features/projects/settingsSections";

/** Where a place is listed: the sidebar's places, the settings area's own pages, or the Admin area. */
export type PlaceArea = "work" | "you" | "admin";

/** The Admin area's sections, in the order its page and its navigation list them. */
export const ADMIN_SECTIONS = ["people", "work", "integrations", "records"] as const;
export type AdminSection = (typeof ADMIN_SECTIONS)[number];

/** A page of this build the reader can go to: the sidebar, the settings area or Admin lists it, the palette finds
 *  it, the breadcrumbs name it. */
export interface Place {
  /** Names the place in data-nav and data-admin-card. */
  id: string;
  path: string;
  icon: IconName;
  label: string;
  area: PlaceArea;
  /** What the page is for, in a sentence, as the settings hub and the Admin page say it. */
  detail?: string;
  /** The Admin section the page sits in. */
  section?: AdminSection;
  /** Also in the avatar menu, among the person's own pages, by this name for the tests. */
  inAccountMenu?: string;
  /** Its tabs are addresses under its path, and each is this page rather than one under it. */
  tabbed?: boolean;
}

/** The settings area: its hub, and every page under it gets the settings sidebar in place of the main one. */
export const SETTINGS_PATH = "/settings";

/** The one entry that leads an administrator to the Admin area, from the sidebar, the avatar menu and the palette. */
export function adminEntry(): Place {
  return { id: "admin", path: ADMIN_PATH, icon: "Settings", label: t.admin.title, area: "admin" };
}

/** The installation's pages, by section, whoever reads; the Admin area itself decides who may open them. */
export function adminPlaces(): Place[] {
  const p = t.admin.pages;
  const place = (id: string, section: AdminSection, path: string, icon: IconName, page: { label: string; detail: string }, tabbed?: boolean): Place => ({
    id,
    area: "admin",
    section,
    path,
    icon,
    label: page.label,
    detail: page.detail,
    tabbed,
  });
  return [
    place("installation", "people", INSTALLATION_PATH, "Settings", p.installation),
    place("access", "people", ACCESS_PATH, "Lock", p.access, true),
    place("users", "people", USERS_PATH, "Users", p.users),
    place("link-types", "work", LINK_TYPES_PATH, "Link", p.linkTypes),
    place("armature", "integrations", ARMATURE_SETTINGS_PATH, "Link", p.armature),
    place("stator", "integrations", STATOR_SETTINGS_PATH, "Link", p.stator),
    place("connected-apps", "integrations", ALL_CONNECTED_APPS_PATH, "Key", p.connectedApps),
    place("webhooks", "integrations", WEBHOOKS_PATH, "Share", p.webhooks),
    place("audit", "records", AUDIT_PATH, "Shield", p.audit),
  ];
}

// A function rather than a list, so the labels are read in the language shown now.
function ownPlaces(): Place[] {
  const own = t.settingsHub.places;
  return [
    { id: "home", path: "/", icon: "Home", label: t.nav.home, area: "work" },
    { id: "search", path: SEARCH_PATH, icon: "Search", label: t.nav.search, area: "work" },
    { id: "notifications", path: NOTIFICATIONS_PATH, icon: "Bell", label: t.notifications.title, area: "work" },
    { id: "reviews", path: REVIEWS_PATH, icon: "Check", label: t.review.inboxTitle, area: "work" },
    { id: "projects", path: PROJECTS_PATH, icon: "Folder", label: t.nav.projects, area: "work" },
    { id: "profile", path: PROFILE_PATH, icon: "User", label: t.account.profile, detail: own.profile, area: "you", inAccountMenu: "profile" },
    {
      id: "notification-settings",
      path: NOTIFICATION_SETTINGS_PATH,
      icon: "Bell",
      label: t.account.notifications,
      detail: own.notifications,
      area: "you",
      inAccountMenu: "notification-settings",
    },
    { id: "watching", path: WATCHING_PATH, icon: "Eye", label: t.account.watching, detail: own.watching, area: "you", inAccountMenu: "watching" },
    { id: "themes", path: THEMES_PATH, icon: "Palette", label: t.account.themes, detail: own.themes, area: "you", inAccountMenu: "themes" },
    { id: "tokens", path: TOKENS_PATH, icon: "Key", label: t.account.tokens, detail: own.tokens, area: "you", inAccountMenu: "tokens" },
    {
      id: "connected-apps-own",
      path: CONNECTED_APPS_PATH,
      icon: "Link",
      label: t.account.connectedApps,
      detail: own.connectedApps,
      area: "you",
      inAccountMenu: "connected-apps",
    },
    { id: "armature-account", path: ARMATURE_ACCOUNT_PATH, icon: "Link", label: t.account.armature, detail: own.armature, area: "you" },
    { id: "stator-account", path: STATOR_ACCOUNT_PATH, icon: "Link", label: t.account.stator, detail: own.stator, area: "you" },
  ];
}

function allPlaces(): Place[] {
  return [...ownPlaces(), ...adminPlaces()];
}

/** Every place the signed-in person may go, in the order the sidebar, the settings area and Admin list them; the
 *  installation's pages only for its administrators, as one check for the whole of Admin. */
export function placesFor(me: Me | undefined): Place[] {
  return me?.administers ? allPlaces() : ownPlaces();
}

/** The heading of each area's places, written before them in the palette. */
export function areaTitle(area: PlaceArea): string {
  if (area === "you") return t.nav.groupYou;
  if (area === "admin") return t.admin.title;
  return t.nav.groupRequirements;
}

/** Whether a path is the settings area's, which draws its own sidebar. */
export function inSettings(pathname: string): boolean {
  return pathname === SETTINGS_PATH || pathname.startsWith(`${SETTINGS_PATH}/`);
}

/** Whether a path is the Admin area's. */
export function inAdmin(pathname: string): boolean {
  return pathname === ADMIN_PATH || pathname.startsWith(`${ADMIN_PATH}/`);
}

/** One crumb of the trail: a place to go back to, or the page itself without a path. */
export interface PlaceCrumb {
  label: string;
  path?: string;
}

/** What a trail needs beyond the address, once it is known. */
export interface CrumbNames {
  projectName?: string;
}

/** The crumbs from the installation to the page at a path, the page itself last; empty for a path that is no place.
 *  In the settings area every trail passes Settings, which leads to its hub, and in Admin it passes Admin. */
export function crumbsFor(pathname: string, names: CrumbNames = {}): PlaceCrumb[] {
  if (pathname === SETTINGS_PATH) return [{ label: t.nav.settings }];
  if (pathname === ADMIN_PATH) return [{ label: t.admin.title }];
  const settings = projectSettingsCrumbs(pathname, names);
  if (settings) return settings;
  const area: PlaceCrumb[] = inSettings(pathname)
    ? [{ label: t.nav.settings, path: SETTINGS_PATH }]
    : inAdmin(pathname)
      ? [{ label: t.admin.title, path: ADMIN_PATH }]
      : [];
  const place = allPlaces().find((each) => each.path === pathname || (each.tabbed && pathname.startsWith(`${each.path}/`)));
  if (place) return [...area, { label: place.label }];
  const above = placeAbove(pathname);
  if (!above) return [];
  return [...area, { label: above.label, path: above.path }];
}

/** Where a project's own pages begin; they all lie under the projects. */
export const PROJECT_PAGES_PREFIX = "/projects/";

/** The project a path lies in, from its /projects/<key> prefix. */
export function projectOfPath(pathname: string): string | undefined {
  const match = /^\/projects\/([^/]+)\/settings(?=\/|$)/.exec(pathname);
  return match ? decodeURIComponent(match[1]!) : undefined;
}

// A project's settings lie under the project, then Settings; a page below a
// section, such as the workflow designer, under Settings as a link.
function projectSettingsCrumbs(pathname: string, names: CrumbNames): PlaceCrumb[] | undefined {
  const key = projectOfPath(pathname);
  if (!key) return undefined;
  const projects = allPlaces().find((each) => each.path === PROJECTS_PATH)!;
  const parts = pathname.split("/").filter(Boolean);
  const below = parts.length > 4;
  return [
    { label: projects.label, path: PROJECTS_PATH },
    { label: names.projectName ?? key, path: `${PROJECT_PAGES_PREFIX}${key}` },
    below ? { label: t.projects.settingsTitle, path: projectSettingsPath(key, sectionOfPath(pathname)) } : { label: t.projects.settingsTitle },
  ];
}

/** The place a page lies under, such as Themes for one theme's editor, when the page is no place itself. */
export function placeAbove(pathname: string): Place | undefined {
  const places = allPlaces();
  if (pathname.startsWith(PROJECT_PAGES_PREFIX)) return places.find((each) => each.path === PROJECTS_PATH);
  return places.find((each) => each.path !== "/" && pathname.startsWith(`${each.path}/`));
}
