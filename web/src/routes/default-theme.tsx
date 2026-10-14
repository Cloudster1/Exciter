import { createRoute } from "@tanstack/react-router";
import { DEFAULT_THEME_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { OrgDefaultTheme } from "@/features/themes/OrgDefaultTheme";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The theme everybody in the installation sees until they choose, for its administrators. */
export const defaultThemeRoute = createRoute({
  getParentRoute: () => appRoute,
  path: DEFAULT_THEME_PATH,
  component: () => (
    <AdminsOnly title={t.themes.orgDefault.title} refusal={t.themes.orgDefault.notAdmin}>
      <OrgDefaultTheme />
    </AdminsOnly>
  ),
});
