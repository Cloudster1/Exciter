import { createRoute, redirect } from "@tanstack/react-router";
import { DEFAULT_THEME_PATH, THEMES_PATH } from "@/config";
import { appRoute } from "./app";

// Everyone's default is named where the themes are; the old address keeps a bookmark working.
export const defaultThemeRoute = createRoute({
  getParentRoute: () => appRoute,
  path: DEFAULT_THEME_PATH,
  beforeLoad: () => {
    throw redirect({ to: THEMES_PATH, replace: true });
  },
});
