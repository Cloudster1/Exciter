import { request, type Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// Test results from CI (#200): a pipeline uploads a JUnit report with an evidence token, the requirement shows its
// result and is verified by it, and a revision makes the result outdated.

const KEY = uniqueKey("TRES");
const COMMIT = "4f2c9a1e7b3d";

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

function report(key: string): Buffer {
  return Buffer.from(
    `<?xml version="1.0"?><testsuites><testsuite name="brakes"><testcase classname="brakes" name="engage within 100 ms [verifies ${key}]" time="0.004"/></testsuite></testsuites>`,
  );
}

/** Uploads a report as a pipeline does, with the token alone, and waits for the worker to land it. */
async function uploadAsCI(secret: string, key: string) {
  const ci = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  try {
    const made = await ci.post(`/api/v1/projects/${KEY}/test-runs`, {
      multipart: {
        repository: "acme/checkout",
        commit: COMMIT,
        branch: "main",
        runUrl: "https://ci.example/runs/7",
        file: { name: "junit.xml", mimeType: "application/xml", buffer: report(key) },
      },
    });
    expect(made.status()).toBe(201);
    const id = (await made.json()).run.id as string;
    await expect.poll(async () => (await (await ci.get(`/api/v1/test-runs/${id}`)).json()).run.state, { timeout: 30_000 }).toBe("done");
  } finally {
    await ci.dispose();
  }
}

/** Reads the coverage report until it says what is expected: the worker clears its cache once it hears of a change. */
async function coverageReads(page: Page, said: string) {
  await page.goto(`/projects/${KEY}/coverage`);
  await expect(async () => {
    await page.reload();
    await expect(page.getByRole("list", { name: "Requirements" })).toContainText(said, { timeout: 2_000 });
  }).toPass({ timeout: 30_000 });
}

test.describe("test results", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a pipeline's result verifies a requirement, and a revision outdates it", async ({ page, api }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Tested" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    must(
      await api.PUT("/projects/{projectKey}/coverage-rules", {
        ...project,
        body: { decomposed: false, implemented: false, verified: true, verifiedFrom: ["tests"] },
      }),
    );
    const token = must(await api.POST("/tokens", { body: { name: `ci ${KEY}`, scopes: ["evidence"] } })).token;
    await uploadAsCI(token.secret!, `${KEY}-R1`);

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const panel = page.locator("[data-test-results]");
    await expect(panel.getByRole("heading", { name: "On main, which counts" })).toBeVisible();
    await expect(panel.locator(`[data-test-result="engage within 100 ms [verifies ${KEY}-R1]"]`)).toHaveAttribute("data-outcome", "passed");
    await expect(panel.getByRole("link", { name: "Run" })).toHaveAttribute("href", "https://ci.example/runs/7");
    await expectAccessible(page);

    await coverageReads(page, "Verified: Full");

    must(
      await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` } }, body: { title: "Brakes engage within 80 ms", baseRevision: 1 } }),
    );
    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(panel.getByText("Outdated: run against revision 1")).toBeVisible();
    await coverageReads(page, "Verified: Partial");
    await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  });
});
