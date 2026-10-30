import { readFileSync } from "node:fs";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { PRINTED } from "../fixtures/stack";

// Test executions (#251): alice starts an execution of a plan, runs its case step by step, fails a step with a
// file, closes it and downloads its report.

const KEY = uniqueKey("TEX");
const paragraph = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("test executions", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice runs a test case step by step and closes the execution", async ({ page, api }) => {
    test.setTimeout(90_000);
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));
    const type = must(
      await api.POST("/projects/{projectKey}/types", { ...path, body: { name: "Test case", icon: "requirement", colour: "#4f6bed", role: "testCase" } }),
    ).type;
    const made = must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: {
          title: "Brake on a wet road",
          typeId: type.id,
          testCase: {
            preconditions: paragraph("The car stands on a wet road."),
            steps: [
              { action: paragraph("Drive at 50 km/h."), expected: null },
              { action: paragraph("Press the pedal."), expected: paragraph("The car stops.") },
            ],
            parameters: [],
          },
        },
      }),
    ).requirement;
    const me = must(await api.GET("/auth/me")).user;

    await page.goto(`/projects/${KEY}/tests`);
    await page.getByRole("tab", { name: "Plans" }).click();
    await page.locator('[data-action="new-plan"]').click();
    const form = page.locator("[data-plan-form]");
    await form.getByLabel("Title").fill("Wet road");
    await form.locator('[data-action="make-plan"]').click();
    await expect(page.getByRole("heading", { name: "Wet road" })).toBeVisible();
    await page.getByLabel("Add a test case").selectOption({ label: `${made.key} Brake on a wet road` });
    await page.locator('[data-action="add-plan-case"]').click();
    await expect(page.locator(`[data-plan-case="${made.key}"]`)).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-action="start-execution"]').click();
    const start = page.locator("[data-start-execution]");
    await start.getByLabel("Version").fill("2.3");
    await start.getByRole("combobox", { name: "Add a tester" }).pressSequentially(me.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(me.name) }).click();
    await start.locator('[data-action="start-execution-send"]').click();
    await expect(page.locator("[data-execution-page]")).toBeVisible();
    await expect(page.locator("[data-execution-progress]")).toContainText("0 of 1 run");
    await expectAccessible(page);

    await page.getByRole("link", { name: new RegExp(`Run ${made.key}`) }).click();
    await expect(page.getByText("The car stands on a wet road.")).toBeVisible();
    await page.getByRole("group", { name: "Outcome of step 1" }).getByRole("button", { name: "Passed" }).click();
    await page.getByRole("group", { name: "Outcome of step 2" }).getByRole("button", { name: "Failed" }).click();
    await page.locator('[data-step-note="2"]').fill("It slid on.");
    await page.locator('[data-step-file="2"]').setInputFiles({ name: "slide.txt", mimeType: "text/plain", buffer: Buffer.from("skid marks") });
    await expect(page.locator('[data-run-file="slide.txt"]')).toBeVisible();
    await page.locator('[data-action="record-results"]').click();
    await expect(page.locator("[data-case-outcome]")).toHaveAttribute("data-case-outcome", "failed");
    await expectAccessible(page);

    await page.goto(page.url().replace(/\/run\/.*$/, ""));
    await expect(page.locator("[data-execution-progress]")).toContainText("1 of 1 run, 1 failed");
    await page.locator('[data-action="close-execution"]').click();
    await page.getByRole("dialog").locator('[data-action="close-execution-send"]').click();
    await expect(page.locator("[data-execution-page]")).toHaveAttribute("data-execution-state", "closed");

    const download = page.waitForEvent("download", PRINTED);
    await page.locator('[data-action="export-menu"]').click();
    await page.getByRole("menuitem", { name: "PDF" }).click();
    const saved = await download;
    expect(
      readFileSync(await saved.path())
        .subarray(0, 5)
        .toString(),
    ).toBe("%PDF-");
  });
});
