import { createRoute } from "@tanstack/react-router";
import { ALL_CONNECTED_APPS_PATH, CONNECTED_APPS_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { AllConnectedApps } from "@/features/oauth/AllConnectedApps";
import { ConnectedApps } from "@/features/oauth/ConnectedApps";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** A person's own connected apps, under their account beside the tokens. */
export const connectedAppsRoute = createRoute({ getParentRoute: () => appRoute, path: CONNECTED_APPS_PATH, component: ConnectedApps });

/** Every connected app of the installation, for its administrators. */
export const allConnectedAppsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ALL_CONNECTED_APPS_PATH,
  component: () => (
    <AdminsOnly title={t.oauth.allTitle} refusal={t.oauth.notAdmin}>
      <AllConnectedApps />
    </AdminsOnly>
  ),
});
