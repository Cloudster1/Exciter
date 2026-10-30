import { randomBytes } from "node:crypto";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const INSTALLATION_PATH = "/admin/installation";
const CONNECTED_APPS_PATH = "/admin/connected-apps";
const DISCOVERY = "/.well-known/oauth-protected-resource";
// Nothing listens there; with OAuth switched off the browser never goes back to it.
const CALLBACK = "http://127.0.0.1:9/callback";
const SWITCHED_OFF = "Apps cannot connect to this server with OAuth right now; its administrators switched it off.";

/** A client of this spec's own, put straight into the database, so registering's count by address is never met. */
async function aClient(): Promise<string> {
  const id = `e2e-${randomBytes(8).toString("hex")}`;
  await withDatabase((db) =>
    db.query(
      `INSERT INTO oauth_client (id, kind, name, redirect_uris, grant_types) VALUES ($1, 'registered', 'Switched off', ARRAY[$2], ARRAY['authorization_code'])`,
      [id, CALLBACK],
    ),
  );
  return id;
}

// The name is everybody's, so the spec runs alone and puts the one it found back.
test.describe("the installation", { tag: "@desktop" }, () => {
  let before = "";

  test.beforeEach(async ({ api }) => {
    before = must(await api.GET("/installation")).installation.name;
  });
  test.afterEach(async ({ api }) => {
    if (before) must(await api.PUT("/installation", { body: { name: before } }));
  });

  test("an administrator renames it, and every sidebar shows the new name", async ({ page, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "Requirements");
    await page.goto(INSTALLATION_PATH);
    await expect(page.locator("main").getByRole("heading", { level: 1, name: "Installation" })).toBeVisible();
    const field = page.getByLabel("Name", { exact: true });
    await expect(field).toHaveValue(before);
    await expectAccessible(page);

    await field.fill(name);
    await page.locator('[data-action="rename-installation"]').click();
    await expect(page.locator("[data-installation-notice]")).toHaveText("Saved.");
    await expect(page.locator("[data-shell-strip]").getByRole("link", { name, exact: true })).toBeVisible();
    await page.goto("/");
    await expect(page.locator("[data-sidebar-org]")).toHaveText(name);

    // Alice renamed it, so bob's read may come from a replica a moment behind.
    const bob = await pageAs("bob");
    await openShowing(bob, "/", bob.locator("[data-sidebar-org]", { hasText: name }));
    await bob.goto(INSTALLATION_PATH);
    await expect(bob.getByText("Only administrators can open this page")).toBeVisible();
  });

  test("an administrator switches OAuth off: discovery goes and an app asking to connect is told why", async ({ page, api }) => {
    const client = await aClient();
    try {
      await page.goto(CONNECTED_APPS_PATH);
      const card = page.locator("[data-oauth-connections]");
      await expect(card).toHaveAttribute("data-oauth-connections", "on");
      await card.getByRole("switch", { name: "Let assistants connect with OAuth" }).click();
      await expect(card).toHaveAttribute("data-oauth-connections", "off");
      await expect(card.getByRole("switch", { name: "Let assistants connect with OAuth" })).toHaveAttribute("aria-checked", "false");
      expect(must(await api.GET("/installation")).installation.oauthConnections).toBe(false);

      // Anybody asks, so the answer may come from a replica a moment behind the switch.
      await expect.poll(async () => (await page.request.get(DISCOVERY)).status()).toBe(404);
      const ask = new URLSearchParams({
        response_type: "code",
        client_id: client,
        redirect_uri: CALLBACK,
        state: "the-state",
        code_challenge: "x".repeat(43),
        code_challenge_method: "S256",
        scope: "mcp",
      });
      await page.goto(`/api/v1/oauth/authorize?${ask}`);
      await expect(page).toHaveURL(/\/oauth\/consent\?error=switched_off$/);
      await expect(page.getByText(SWITCHED_OFF)).toBeVisible();
    } finally {
      must(await api.PUT("/installation/oauth-connections", { body: { enabled: true } }));
      await withDatabase((db) => db.query("DELETE FROM oauth_client WHERE id = $1", [client]));
    }
    await expect.poll(async () => (await page.request.get(DISCOVERY)).status()).toBe(200);
  });
});
