import { createRoute } from "@tanstack/react-router";
import { LINK_TYPES_PATH, MEMBERS_PATH, PROFILE_PATH } from "@/config";
import { ProfilePage } from "@/features/account/ProfilePage";
import { SettingsHub } from "@/features/account/SettingsHub";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { Members } from "@/features/auth/Members";
import { OrgLinkTypes } from "@/features/links/LinkTypeList";
import { SETTINGS_PATH } from "@/features/shell/places";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The settings area's hub: the person's own pages, and the installation's for its administrators. */
export const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: SETTINGS_PATH, component: SettingsHub });

/** Your profile: who the person is here, and the language they read in. */
export const profileRoute = createRoute({ getParentRoute: () => appRoute, path: PROFILE_PATH, component: ProfilePage });

/** The installation's people and their roles, for its administrators. */
export const membersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: MEMBERS_PATH,
  component: () => (
    <AdminsOnly title={t.members.title} refusal={t.members.notAdmin}>
      <Members />
    </AdminsOnly>
  ),
});

/** The installation's link types, for its administrators. */
export const linkTypesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: LINK_TYPES_PATH,
  component: () => (
    <AdminsOnly title={t.linkTypes.title} refusal={t.linkTypes.notAdmin}>
      <OrgLinkTypes />
    </AdminsOnly>
  ),
});
