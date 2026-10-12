import { administers, type Me } from "@/api/auth";
import type { IconName } from "@/components/icons";
import { AUDIT_PATH, MEMBERS_PATH } from "@/config";
import { t } from "@/i18n";

/** A page of this build the reader can go to: the sidebar lists it, the quick switcher finds it, the breadcrumbs name it. */
export interface Place {
  path: string;
  icon: IconName;
  label: string;
  /** The group it sits in, written before it in the breadcrumbs. */
  section?: string;
  /** Kept for the organization's owners and administrators. */
  adminOnly?: boolean;
}

/** Where the settings rail entry leads; the router sends it on to the first settings page. */
export const SETTINGS_PATH = "/settings";

// A function rather than a list, so the labels are read in the language shown now.
function allPlaces(): Place[] {
  return [
    { path: "/", icon: "Home", label: t.nav.home },
    { path: MEMBERS_PATH, icon: "Users", label: t.nav.members, section: t.nav.settings, adminOnly: true },
    { path: AUDIT_PATH, icon: "Shield", label: t.nav.audit, section: t.nav.settings, adminOnly: true },
  ];
}

/** Every place the signed-in person may go, in the order the sidebar lists them. */
export function placesFor(me: Me | undefined): Place[] {
  const admin = administers(me?.organization?.role);
  return allPlaces().filter((place) => admin || !place.adminOnly);
}

/** The crumbs from the organization to the page at a path, the page itself last; empty for a path that is no place. */
export function crumbsFor(pathname: string): string[] {
  const place = allPlaces().find((each) => each.path === pathname);
  if (!place) return [];
  return place.section ? [place.section, place.label] : [place.label];
}
