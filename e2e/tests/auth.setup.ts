import { test as setup } from "@playwright/test";
import { saveSession } from "../fixtures/auth";
import { clearInstallationThemes } from "../fixtures/seed";

// Signs everybody in once, before any spec, so each spec starts from a stored session instead of walking through
// Keycloak itself. The installation then shows the built-in theme, whatever an earlier run left there.
setup("sign in alice and bob", async ({ browser }) => {
  await saveSession(browser, "alice");
  await saveSession(browser, "bob");
  await clearInstallationThemes();
});
