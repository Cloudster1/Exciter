import { defineConfig, devices, type ReporterDescription } from "@playwright/test";
import { WEB_URL } from "./fixtures/stack";

// A spec that only means something at one width carries @desktop or @mobile
// and runs in that project alone; everything else runs in both.
const MOBILE_VIEWPORT = { width: 360, height: 740 };

// The specs that change what the whole installation shares, its name, provider, default theme or Armature and
// Stator connections, run one at a time beside the rest, which keep to their own people and projects.
const SERIAL =
  /\/(sso|themes|armature|armature-links|armature-create|armature-webhooks|armature-theme|stator|stator-links|installation|connect-at-sign-in)\.spec\.ts$/;

// CI's browser jobs each leave a blob that its report job merges (merge.config.ts); anywhere
// else the suite writes the HTML report and the JSON the gate's summary reads itself.
const REPORTS_DIR = "../reports";
const reporter: ReporterDescription[] = process.env.CI
  ? [["list"], ["blob", { outputDir: `${REPORTS_DIR}/e2e-blob` }]]
  : [["list"], ["html", { outputFolder: "playwright-report", open: "never" }], ["json", { outputFile: `${REPORTS_DIR}/e2e.json` }]];

const TEST_TIMEOUT_MS = 20_000;
const EXPECT_TIMEOUT_MS = 5_000;
const ACTION_TIMEOUT_MS = 5_000;
const NAVIGATION_TIMEOUT_MS = 10_000;

export default defineConfig({
  testDir: "./tests",
  outputDir: "./test-results",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: TEST_TIMEOUT_MS,
  expect: { timeout: EXPECT_TIMEOUT_MS },
  reporter,
  use: {
    baseURL: WEB_URL,
    // The interface follows the browser's language, and the specs read its English words.
    locale: "en-US",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: ACTION_TIMEOUT_MS,
    navigationTimeout: NAVIGATION_TIMEOUT_MS,
  },
  projects: [
    { name: "setup", testMatch: /.*\.setup\.ts/, teardown: "teardown" },
    { name: "teardown", testMatch: /.*\.teardown\.ts/ },
    {
      name: "chromium",
      dependencies: ["setup"],
      testIgnore: SERIAL,
      grepInvert: /@mobile/,
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "mobile",
      dependencies: ["setup"],
      testIgnore: SERIAL,
      grepInvert: /@desktop/,
      use: { ...devices["Desktop Chrome"], viewport: MOBILE_VIEWPORT, isMobile: true, hasTouch: true },
    },
    {
      name: "serial",
      dependencies: ["setup"],
      testMatch: SERIAL,
      workers: 1,
      grepInvert: /@mobile/,
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
