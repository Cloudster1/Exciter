import { uniqueKey } from "../fixtures/seed";
import { request } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible, openAccountMenu } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const KEY = uniqueKey("TOKS");

// The tokens are this spec's own alice's, and the project's key this worker's, so they meet no other spec's.
test.describe("personal access tokens", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
    for (const token of (await api.GET("/tokens")).data?.tokens ?? []) {
      await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
    }
  });

  test("alice makes a read-only token, a script reads with it and is refused a write, and she revokes it", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Tokens" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Read by a script" } }));

    await page.goto("/");
    await openAccountMenu(page);
    await page.getByRole("menuitem", { name: "Access tokens" }).click();
    await expect(page).toHaveURL(/\/settings\/tokens$/);
    await page.getByRole("textbox", { name: "Token name" }).fill("Nightly export");
    await page.getByRole("checkbox", { name: "Read only" }).check();
    await page.locator(`[data-project="${KEY}"]`).click();
    await page.locator('[data-action="create-token"]').click();
    const secret = await page.locator("[data-token-secret]").inputValue();
    expect(secret).toMatch(/^exciter_pat_/);
    await expect(page.locator('[data-token-row="Nightly export"]')).toContainText(`Only ${KEY}`);
    await expectAccessible(page);

    const script = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
    try {
      // The script has no write of its own to read back, so it may meet a replica that has not caught up yet.
      await expect.poll(async () => (await script.get(`/api/v1/requirements/${KEY}-R1`)).status()).toBe(200);
      expect((await (await script.get(`/api/v1/requirements/${KEY}-R1`)).json()).requirement.title).toBe("Read by a script");
      const write = await script.post(`/api/v1/projects/${KEY}/requirements`, { data: { title: "Not allowed" } });
      expect(write.status()).toBe(403);
      expect((await write.json()).error.code).toBe("read_only_token");
      const tools = await script.post("/api/v1/mcp", { data: { jsonrpc: "2.0", id: 1, method: "tools/list" } });
      const names = (await tools.json()).result.tools.map((tool: { name: string }) => tool.name);
      expect(names).toContain("get_requirement");
      expect(names).not.toContain("create_requirement");

      page.on("dialog", (dialog) => void dialog.accept());
      await page.getByRole("button", { name: "Revoke Nightly export" }).click();
      await expect(page.getByText("Nightly export was revoked.")).toBeVisible();
      await expect.poll(async () => (await script.get("/api/v1/auth/me")).status()).toBe(401);
    } finally {
      await script.dispose();
    }
  });
});
