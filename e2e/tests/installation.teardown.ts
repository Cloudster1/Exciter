import { test as teardown } from "@playwright/test";
import { clearInstallationThemes } from "../fixtures/seed";

// After every spec, so a person opening the stack, or the next run, finds it as the seed left it.
teardown("the installation is left with the built-in theme", async () => {
  await clearInstallationThemes();
});
