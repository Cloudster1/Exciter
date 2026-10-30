import { createRoute } from "@tanstack/react-router";
import { LINK_TYPES_PATH, PROFILE_PATH } from "@/config";
import { ProfilePage } from "@/features/account/ProfilePage";
import { SettingsHub } from "@/features/account/SettingsHub";
import { OrgLinkTypes } from "@/features/links/LinkTypeList";
import { SETTINGS_PATH } from "@/features/shell/places";
import { adminRoute } from "./admin";
import { appRoute } from "./app";

/** The settings area's hub: the person's own pages. */
export const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: SETTINGS_PATH, component: SettingsHub });

/** Your profile: who the person is here, and the language they read in. */
export const profileRoute = createRoute({ getParentRoute: () => appRoute, path: PROFILE_PATH, component: ProfilePage });

/** The installation's link types, for its administrators. */
export const linkTypesRoute = createRoute({ getParentRoute: () => adminRoute, path: LINK_TYPES_PATH, component: OrgLinkTypes });
