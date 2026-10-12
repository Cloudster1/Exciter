import { test as teardown } from "@playwright/test";
import { clearDemoThemes } from "../fixtures/seed";

// After every spec, so a person opening the stack's demo, or the next run, finds it as the seed left it.
teardown("demo is left with the built-in theme", async () => {
  await clearDemoThemes();
});
