import { request } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, confirm, openAdmin, toast, dismissToasts } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// The controls of /settings/tokens and of everybody's tokens under Admin, Connected apps, no other spec clicks (#329): copying a fresh token,
// putting it away for good, and an administrator revoking somebody else's.

// web/src/config.ts TOKENS_PATH, and the prefix of a personal access token, backend/internal/auth.
const TOKENS_PATH = "/settings/tokens";
const TOKEN_PREFIX = "exciter_pat_";

async function revokeAll(api: ExciterApi) {
  for (const token of (await api.GET("/tokens")).data?.tokens ?? []) {
    await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  }
}

/** How the API answers a script holding a secret: whose it is, or refused. */
async function meStatus(secret: string): Promise<number> {
  const script = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  try {
    return (await script.get("/api/v1/auth/me")).status();
  } finally {
    await script.dispose();
  }
}

test.describe("token controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api, apiAs }) => {
    await revokeAll(api);
    await revokeAll(await apiAs("bob"));
  });

  test("alice copies a fresh token, which works, and once she is done no reload shows it again", async ({ page }, testInfo) => {
    const name = uniqueName(testInfo, "Copied");
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto(TOKENS_PATH);
    await page.getByRole("textbox", { name: "Token name" }).fill(name);
    await page.locator('[data-action="create-token"]').click();
    const fresh = page.locator("[data-fresh-token]");
    await expect(fresh.locator("[data-token-secret]")).toHaveValue(new RegExp(`^${TOKEN_PREFIX}`));

    await fresh.locator('[data-action="copy-token"]').click();
    await expect(fresh.locator("[data-copy-status]")).toHaveText("Copied the token.");
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    expect(copied).toBe(await fresh.locator("[data-token-secret]").inputValue());
    // A token is read back by another caller than the one that made it, so it may meet a replica still catching up.
    await expect.poll(() => meStatus(copied)).toBe(200);

    await fresh.locator('[data-action="token-done"]').click();
    await expect(fresh).toHaveCount(0);
    await page.reload();
    await expect(page.locator(`[data-token-row="${name}"]`)).toBeVisible();
    await expect(page.locator("[data-fresh-token]")).toHaveCount(0);
    expect(await page.content()).not.toContain(copied);
  });

  test("alice revokes bob's token under Admin, Connected apps, and bob's script is refused", async ({ page, apiAs, people }, testInfo) => {
    const name = uniqueName(testInfo, "Bob's script");
    const bob = await apiAs("bob");
    const secret = must(await bob.POST("/tokens", { body: { name } })).token.secret!;
    await expect.poll(() => meStatus(secret)).toBe(200);

    await openAdmin(page, "connected-apps");
    await expect(page).toHaveURL(/\/admin\/connected-apps$/);
    const row = page.locator(`[data-token-row="${name}"]`);
    await expect(row).toHaveAttribute("data-token-owner", people.bob.email);
    await expectAccessible(page);

    await row.getByRole("button", { name: `Revoke ${name} of ${people.bob.name}` }).click();
    await expect(page.locator("[data-confirm]")).toContainText(`Revoke token ${name}?`);
    await expect(page.locator("[data-confirm]")).toContainText("Anything using it is refused from now on.");
    await confirm(page);
    await expect(toast(page, `${name} was revoked.`)).toHaveText(`${name} was revoked.`);
    await dismissToasts(page);
    await expect(row).toHaveCount(0);
    await expect.poll(() => meStatus(secret)).toBe(401);
    await expect.poll(async () => must(await bob.GET("/tokens")).tokens.map((each) => each.name)).not.toContain(name);
  });
});
