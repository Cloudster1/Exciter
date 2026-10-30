import { createRoute } from "@tanstack/react-router";
import { ALL_CONNECTED_APPS_PATH, CONNECTED_APPS_PATH } from "@/config";
import { InstallationApps } from "@/features/oauth/InstallationApps";
import { ConnectedApps } from "@/features/oauth/ConnectedApps";
import { adminRoute } from "./admin";
import { appRoute } from "./app";

/** A person's own connected apps, under their account beside the tokens. */
export const connectedAppsRoute = createRoute({ getParentRoute: () => appRoute, path: CONNECTED_APPS_PATH, component: ConnectedApps });

/** The installation's connected apps for its administrators: the partner apps, and everybody's grants and tokens. */
export const allConnectedAppsRoute = createRoute({ getParentRoute: () => adminRoute, path: ALL_CONNECTED_APPS_PATH, component: InstallationApps });
