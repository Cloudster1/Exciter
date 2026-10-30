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

    await page.goto(`/projects/${KEY}/r/${KEY}-R1/history`);
    const latest = page.locator('[data-revision-row="2"]');
    await expect(latest.locator('[data-via="token"]')).toContainText(`through ${TOKEN}`);
    await expect(page.locator('[data-revision-row="1"] [data-via]')).toHaveCount(0);
    await expectAccessible(page);
  });

  test("a comment and a review asked for through a token say so where they are read", async ({ page, api }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Through a token" } }));
    for (const title of ["Brakes", "Lights"]) must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title } }));
    const me = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/projects/{projectKey}/approval-rules", {
        ...project,
        body: { required: 1, authorMayApprove: true, approvers: [{ subjectType: "user", userId: me.id }] },
      }),
    );
    const said = (text: string) => ({ body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] } });
    const secret = must(await api.POST("/tokens", { body: { name: TOKEN } })).token.secret!;
    const script = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
    try {
      expect((await script.post(`/api/v1/requirements/${KEY}-R1/comments`, { data: said("Checked overnight.") })).status()).toBe(201);
      expect((await script.post(`/api/v1/requirements/${KEY}-R1/reviews`, { data: { revision: 1 } })).status()).toBe(201);
    } finally {
      await script.dispose();
    }
    const byHand = { params: { path: { reqKey: `${KEY}-R2` } } };
    must(await api.POST("/requirements/{reqKey}/comments", { ...byHand, body: said("Written by hand.") }));
    must(await api.POST("/requirements/{reqKey}/reviews", { ...byHand, body: { revision: 1 } }));

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(page.locator('[data-comment] [data-via="token"]')).toContainText(`through ${TOKEN}`);
    await expect(page.locator('[data-pending-review] [data-via="token"]')).toContainText(`through ${TOKEN}`);
    await expectAccessible(page);
    await page.goto(`/projects/${KEY}/r/${KEY}-R2`);
    await expect(page.locator("[data-comment]")).toContainText("Written by hand.");
    await expect(page.locator("[data-pending-review]")).toBeVisible();
    await expect(page.locator("[data-comment] [data-via], [data-pending-review] [data-via]")).toHaveCount(0);
  });
});
