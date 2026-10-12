import { createRoute, redirect } from "@tanstack/react-router";
import { MEMBERS_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { Members } from "@/features/auth/Members";
import { SETTINGS_PATH } from "@/features/shell/places";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The rail's settings entry: the first settings page, which the address then names. */
export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SETTINGS_PATH,
  beforeLoad: () => {
    throw redirect({ to: MEMBERS_PATH });
  },
});

/** The organization's people and their roles, for its administrators. */
export const membersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: MEMBERS_PATH,
  component: () => (
    <AdminsOnly title={t.members.title} refusal={t.members.notAdmin}>
      <Members />
    </AdminsOnly>
  ),
});
