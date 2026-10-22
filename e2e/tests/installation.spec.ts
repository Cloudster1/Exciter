import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const INSTALLATION_PATH = "/settings/installation";

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
    await expect(bob.getByText("Only an administrator can rename the installation. Ask one of them.")).toBeVisible();
  });
});
