import { request, type Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// Test executions in traceability (#252): a pipeline's result fills the open execution holding the test case it
// verifies, and the requirement the case verifies turns verified from test executions.

const KEY = uniqueKey("TTRC");
const paragraph = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** Uploads a passing result of a case as a pipeline does, with the token alone, and waits for the worker to land it. */
async function uploadAsCI(secret: string, key: string) {
  const ci = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  try {
    const made = await ci.post(`/api/v1/projects/${KEY}/test-runs`, {
      multipart: {
        repository: "acme/checkout",
        commit: "4f2c9a1e7b3d",
        branch: "main",
        file: {
          name: "junit.xml",
          mimeType: "application/xml",
          buffer: Buffer.from(
            `<?xml version="1.0"?><testsuites><testsuite name="brakes"><testcase classname="brakes" name="wet road [verifies ${key}]"/></testsuite></testsuites>`,
          ),
        },
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

test.describe("test executions in traceability", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a pipeline's result fills an open execution and verifies the requirement", async ({ page, api }) => {
    test.setTimeout(90_000);
    const path = { params: { path: { projectKey: KEY } } };
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
          testCase: { preconditions: null, steps: [{ action: paragraph("Press the pedal."), expected: null }], parameters: [] },
        },
      }),
    ).requirement;
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: made.key } }, body: { typeKey: "verifies", otherKey: `${KEY}-R1` } }));
    must(
      await api.PUT("/projects/{projectKey}/coverage-rules", {
        ...path,
        body: { decomposed: false, implemented: false, verified: true, verifiedFrom: ["runs"] },
      }),
    );
    const me = must(await api.GET("/auth/me")).user;
    const plan = must(await api.POST("/projects/{projectKey}/test-plans", { ...path, body: { title: "Wet road", scope: "cases", cases: [made.key] } })).plan;
    const execution = must(
      await api.POST("/projects/{projectKey}/test-executions", {
        ...path,
        body: { title: "Release 1", planId: plan.id, testers: [{ subjectType: "user", userId: me.id }] },
      }),
    ).execution;
    const token = must(await api.POST("/tokens", { body: { name: `ci ${KEY}`, scopes: ["evidence"] } })).token;

    await coverageReads(page, "Verified: Partial");
    await uploadAsCI(token.secret!, made.key);

    await openUntil(page, `/projects/${KEY}/tests/executions/${execution.id}`, () =>
      expect(page.locator("[data-execution-progress]")).toContainText("1 of 1 run, 0 failed", ONE_LOOK),
    );
    await coverageReads(page, "Verified: Full");

    await page.goto(`/projects/${KEY}/matrix`);
    await expect(page.getByRole("rowheader", { name: new RegExp(made.key) }).locator('[data-last-run="passed"]')).toBeVisible();
    await expectAccessible(page);
    await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  });
});
