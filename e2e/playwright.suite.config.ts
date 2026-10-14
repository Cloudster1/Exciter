import { defineConfig, devices } from "@playwright/test";

// The suite's smoke spec (#31): Exciter, Armature and Stator as make suite-up starts them, outside the gate.
const TEST_TIMEOUT_MS = 120_000;
const EXPECT_TIMEOUT_MS = 10_000;

export default defineConfig({
  testDir: "./suite",
  outputDir: "./test-results/suite",
  workers: 1,
  retries: 0,
  timeout: TEST_TIMEOUT_MS,
  expect: { timeout: EXPECT_TIMEOUT_MS },
  reporter: [["list"]],
  use: { ...devices["Desktop Chrome"], locale: "en-US", trace: "retain-on-failure", screenshot: "only-on-failure" },
});
