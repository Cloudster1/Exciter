import { request } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect, test } from "../fixtures/auth";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const KEY = uniqueKey("VIA");
const TOKEN = "Nightly script";

/** Archives and deletes the spec's project and alice's tokens, so each test starts without them. */
async function tidy(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
  for (const token of (await api.GET("/tokens")).data?.tokens ?? []) {
    if (token.name === TOKEN) await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  }
}

// The project's key is this worker's own, and the token is named for this spec, so they meet no other spec.
test.describe("what a token wrote", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => tidy(api));

  test("a requirement changed through a token says so, and by which, in its history", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Through a token" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes" } }));
    const secret = must(await api.POST("/tokens", { body: { name: TOKEN } })).token.secret!;
    const script = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
    try {
      const changed = await script.patch(`/api/v1/requirements/${KEY}-R1`, { data: { title: "Brakes engage", baseRevision: 1 } });
      expect(changed.status()).toBe(200);
    } finally {
      await script.dispose();
    }

    await page.goto(`/p/${KEY}/r/${KEY}-R1/history`);
    const latest = page.locator('[data-revision-row="2"]');
    await expect(latest.locator('[data-via="token"]')).toContainText(`through ${TOKEN}`);
    await expect(page.locator('[data-revision-row="1"] [data-via]')).toHaveCount(0);
    await expectAccessible(page);
  });
});
