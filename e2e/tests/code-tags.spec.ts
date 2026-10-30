import { request } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// Code tags (#201): a pipeline uploads the tags a scan of its checkout found with an evidence token, the requirement
// shows the file and line that implement it, and the project lists a tag that names no requirement.

const KEY = uniqueKey("CTAG");
const COMMIT = "4f2c9a1e7b3d";

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** Uploads a scan as exciter-ci scan does, with the token alone. */
async function scanAsCI(secret: string) {
  const ci = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  try {
    const made = await ci.post(`/api/v1/projects/${KEY}/code-scans`, {
      data: {
        repository: "acme/checkout",
        commit: COMMIT,
        branch: "main",
        fileUrl: "https://git.example/acme/checkout/blob/{commit}/{path}#L{line}",
        tags: [
          { path: "brakes/engage.go", line: 12, word: "implements", key: `${KEY}-R1` },
          { path: "brakes/ghost.go", line: 3, word: "implements", key: `${KEY}-R9` },
        ],
      },
    });
    expect(made.status()).toBe(201);
  } finally {
    await ci.dispose();
  }
}

test.describe("code tags", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a pipeline's scan shows the code on its requirement, and a tag naming none on the project", async ({ page, api }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Scanned" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    const token = must(await api.POST("/tokens", { body: { name: `scan ${KEY}`, scopes: ["evidence"] } })).token;
    await scanAsCI(token.secret!);

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const panel = page.locator("[data-code-links]");
    await expect(panel.getByRole("link", { name: "brakes/engage.go:12" })).toHaveAttribute(
      "href",
      `https://git.example/acme/checkout/blob/${COMMIT}/brakes/engage.go#L12`,
    );
    await expect(panel.getByText("Implements")).toBeVisible();
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/code-tags`);
    const table = page.getByRole("table", { name: "Code tags" });
    await expect(table.locator('[data-code-tag="brakes/ghost.go:3"]')).toHaveAttribute("data-state", "orphaned");
    await expect(table).toContainText("names no requirement");
    await expectAccessible(page);
    await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  });
});
