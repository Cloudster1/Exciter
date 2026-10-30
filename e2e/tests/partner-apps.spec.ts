import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, confirm, toast, dismissToasts } from "../fixtures/shell";

// Partner apps (#273): alice registers one on the installation page, copies its client id and secret once, lets it
// write and removes it. The exchange itself is the integration suite's, which holds a token of the realm.

// web/src/config.ts ALL_CONNECTED_APPS_PATH, and the prefix of a partner's secret, backend/internal/oauth.
const CONNECTED_APPS_PATH = "/admin/connected-apps";
const SECRET_PREFIX = "exciter_ocs_";

async function removeNamed(api: ExciterApi, name: string) {
  for (const app of must(await api.GET("/oauth/clients")).clients.filter((each) => each.name === name)) {
    await api.DELETE("/oauth/clients/{clientID}", { params: { path: { clientID: app.id } } });
  }
}

test.describe("partner apps", { tag: "@desktop" }, () => {
  let name = "";
  test.afterEach(async ({ api }) => {
    if (name) await removeNamed(api, name);
  });

  test("alice registers a partner app, sees its secret once, lets it write and removes it", async ({ page, api }, testInfo) => {
    name = uniqueName(testInfo, "Wiki");
    await page.goto(CONNECTED_APPS_PATH);
    const form = page.getByRole("form", { name: "Register a partner app" });
    await form.getByLabel("App name", { exact: true }).fill(name);
    await form.getByLabel("Keycloak client", { exact: true }).fill("stator");
    await form.locator('[data-action="add-partner"]').click();

    const secretField = page.getByLabel(`Secret of ${name}`, { exact: true });
    await expect(secretField).toHaveValue(new RegExp(`^${SECRET_PREFIX}`));
    const secret = await secretField.inputValue();
    await expectAccessible(page);
    await page.locator('[data-action="partner-secret-done"]').click();
    const row = page.locator(`[data-partner-row="${name}"]`);
    await expect(row).toContainText("Exchanges sign-ins of the Keycloak client stator");
    await expect(row).toContainText("Read only");
    await page.reload();
    await expect(row).toBeVisible();
    expect(await page.content()).not.toContain(secret);

    // Clicked rather than checked: the box is the saved scopes', which arrive after the click.
    await row.locator('[data-action="partner-write"]').click();
    await expect(row.locator('[data-action="partner-write"]')).toBeChecked();
    await expect(row).not.toContainText("Read only");
    const saved = must(await api.GET("/oauth/clients")).clients.find((each) => each.name === name);
    expect(saved?.scopes).toEqual(["read", "write"]);

    await row.locator('[data-action="remove-partner"]').click();
    await confirm(page);
    await expect(toast(page, `${name} was removed.`)).toHaveText(`${name} was removed.`);
    await dismissToasts(page);
    await expect(row).toHaveCount(0);
  });
});
