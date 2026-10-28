import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Running a test execution past the first case (#324): alice finds it at home, changes it, aborts it so coverage
// forgets it, prints it, and walks a case's iterations in the step runner.

const KEY = uniqueKey("TER");
const paragraph = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const path = { params: { path: { projectKey: KEY } } };
// The worker clears the coverage cache once it hears of a change, so the report is read again until it says so.
const COVERAGE_SETTLES = { timeout: 30_000 } as const;

async function removeProject(api: ExciterApi) {
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** A project with a requirement and a test case verifying it, an execution of a plan of the case that alice tests,
 * and the case's key. */
async function anExecution(api: ExciterApi, testCase: { parameters: { name: string; values: string[] }[] }) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } }));
  const type = must(
    await api.POST("/projects/{projectKey}/types", { ...path, body: { name: "Test case", icon: "requirement", colour: "#4f6bed", role: "testCase" } }),
  ).type;
  must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop within 40 m" } }));
  const made = must(
    await api.POST("/projects/{projectKey}/requirements", {
      ...path,
      body: {
        title: "Brake on a wet road",
        typeId: type.id,
        testCase: {
          preconditions: null,
          steps: [
            { action: paragraph("Drive at the speed."), expected: null },
            { action: paragraph("Press the pedal."), expected: paragraph("The car stops.") },
          ],
          parameters: testCase.parameters,
        },
      },
    }),
  ).requirement;
  must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: made.key } }, body: { typeKey: "verifies", otherKey: `${KEY}-R1` } }));
  const me = must(await api.GET("/auth/me")).user;
  const plan = must(await api.POST("/projects/{projectKey}/test-plans", { ...path, body: { title: "Wet road", scope: "cases", cases: [made.key] } })).plan;
  const execution = must(
    await api.POST("/projects/{projectKey}/test-executions", {
      ...path,
      body: {
        title: "Release 1 on wet roads",
        planId: plan.id,
        version: "2.3",
        configuration: "Winter tyres",
        testers: [{ subjectType: "user", userId: me.id }],
      },
    }),
  ).execution;
  return { caseKey: made.key, execution };
}

async function coverageReads(page: Page, said: string) {
  await page.goto(`/p/${KEY}/coverage`);
  await expect(async () => {
    await page.reload();
    await expect(page.locator(`[data-coverage-row="${KEY}-R1"]`)).toContainText(said, { timeout: 2_000 });
  }).toPass(COVERAGE_SETTLES);
}

test.describe("running a test execution", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice changes an execution, aborts it so it verifies nothing, and prints it", async ({ page, api }) => {
    test.setTimeout(90_000);
    const { caseKey, execution } = await anExecution(api, { parameters: [] });
    must(
      await api.PUT("/projects/{projectKey}/coverage-rules", {
        ...path,
        body: { decomposed: false, implemented: false, verified: true, verifiedFrom: ["runs"] },
      }),
    );
    must(
      await api.PUT("/test-executions/{executionID}/cases/{reqKey}/result", {
        params: { path: { executionID: execution.id, reqKey: caseKey } },
        body: {
          iteration: 1,
          steps: [
            { step: 1, outcome: "passed" },
            { step: 2, outcome: "passed", note: "Stopped at 38 m." },
          ],
        },
      }),
    );

    await page.goto("/");
    const mine = page.getByRole("table", { name: "Your test executions" });
    await mine.getByRole("link", { name: "Release 1 on wet roads" }).click();
    await expect(page.locator("[data-execution-page]")).toHaveAttribute("data-execution-page", execution.id);

    await page.locator('[data-action="edit-execution"]').click();
    const edit = page.getByRole("dialog", { name: "Change the execution" });
    await edit.getByLabel("Version").fill("2.4");
    await edit.getByLabel("Configuration").fill("Summer tyres");
    await expectAccessible(page);
    await edit.locator('[data-action="save-execution"]').click();
    await expect(edit).toBeHidden();
    await expect(page.getByText(/Version: 2\.4 · Configuration: Summer tyres/)).toBeVisible();
    await coverageReads(page, "Verified: Full");

    await page.goto(`/p/${KEY}/tests/executions/${execution.id}`);
    await page.locator('[data-action="close-execution"]').click();
    const close = page.getByRole("dialog", { name: "Close the test execution" });
    await close.getByRole("checkbox", { name: "Abort it, so its results count for nothing" }).check();
    await close.locator('[data-action="close-execution-send"]').click();
    await expect(page.locator("[data-execution-page]")).toHaveAttribute("data-execution-state", "aborted");
    await coverageReads(page, "Verified: Partial");

    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Your projects" })).toBeVisible();
    await expect(page.locator('[data-execution="Release 1 on wet roads"]')).toHaveCount(0);

    await page.goto(`/print/executions/${execution.id}`);
    const report = page.locator("[data-print-view]");
    await expect(report.getByRole("heading", { level: 1, name: "Release 1 on wet roads" })).toBeVisible();
    for (const line of ["Version: 2.4", "Configuration: Summer tyres", "Aborted"]) await expect(report).toContainText(line);
    const entry = report.locator(`[data-print-case="${caseKey}#1"]`);
    await expect(entry.getByRole("heading", { name: `${caseKey} Brake on a wet road` })).toBeVisible();
    await expect(entry).toContainText("Passed");
    await expect(entry.getByRole("row", { name: /Press the pedal/ })).toContainText("Stopped at 38 m.");
    await expect(page.locator("html")).toHaveAttribute("data-print-ready", /.*/);
  });

  test("alice walks a case's iterations, keeping what each came to, and overrides the steps", async ({ page, api }) => {
    test.setTimeout(60_000);
    const { caseKey, execution } = await anExecution(api, { parameters: [{ name: "Speed", values: ["30", "50"] }] });
    await page.goto(`/p/${KEY}/tests/executions/${execution.id}/run/${caseKey}?iteration=1`);
    const outcome = page.locator("[data-case-outcome]");
    const step = (n: number) => page.getByRole("group", { name: `Outcome of step ${n}` });

    await expect(page.getByText("Iteration 1 of 2 · Speed = 30")).toBeVisible();
    await expect(page.locator('[data-action="previous-case"]')).toHaveCount(0);
    await step(1).getByRole("button", { name: "Passed" }).click();
    await step(2).getByRole("button", { name: "Passed" }).click();
    await page.locator('[data-step-file="1"]').setInputFiles({ name: "speedo.txt", mimeType: "text/plain", buffer: Buffer.from("30 km/h") });
    await expect(page.locator('[data-run-file="speedo.txt"]')).toBeVisible();
    await page.getByRole("button", { name: "Delete speedo.txt" }).click();
    await expect(page.locator('[data-run-file="speedo.txt"]')).toHaveCount(0);
    await page.locator('[data-action="record-results"]').click();
    await expect(outcome).toHaveAttribute("data-case-outcome", "passed");

    await page.locator('[data-action="next-case"]').click();
    await expect(page.getByText("Iteration 2 of 2 · Speed = 50")).toBeVisible();
    await expect(page.locator('[data-action="next-case"]')).toHaveCount(0);
    await step(1).getByRole("button", { name: "Failed" }).click();
    await page.getByLabel("Outcome of the case").selectOption({ label: "Blocked" });
    await expectAccessible(page);
    await page.locator('[data-action="record-results"]').click();
    await expect(outcome).toHaveAttribute("data-case-outcome", "blocked");

    await page.locator('[data-action="previous-case"]').click();
    await expect(page.getByText("Iteration 1 of 2 · Speed = 30")).toBeVisible();
    await expect(outcome).toHaveAttribute("data-case-outcome", "passed");
    await expect(page.locator('[data-run-step="1"]')).toHaveAttribute("data-step-outcome", "passed");
    await expect(page.locator('[data-run-file="speedo.txt"]')).toHaveCount(0);

    await page.locator('[data-action="next-case"]').click();
    await expect(outcome).toHaveAttribute("data-case-outcome", "blocked");
    await expect(page.locator('[data-run-step="1"]')).toHaveAttribute("data-step-outcome", "failed");

    await page.goto(`/p/${KEY}/tests/executions/${execution.id}`);
    await expect(page.locator(`[data-case-run="${caseKey}#1"]`)).toHaveAttribute("data-outcome", "passed");
    await expect(page.locator(`[data-case-run="${caseKey}#2"]`)).toHaveAttribute("data-outcome", "blocked");
  });
});
