import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, openAccountMenu, openShell } from "../fixtures/shell";
import { armatureURL, statorURL } from "../fixtures/stack";

// The avatar menu links the tools the installation is connected to (#298); the connections are the installation's,
// so the spec runs alone.

/** The tools the open account menu offers to switch to, by name and address. */
async function switchTo(page: Page): Promise<Array<{ name: string; href: string | null }>> {
  const links = page.getByRole("menu").getByRole("group", { name: "Switch to" }).getByRole("menuitem");
  return Promise.all((await links.all()).map(async (link) => ({ name: (await link.innerText()).trim(), href: await link.getAttribute("href") })));
}

/** Opens the menu until it lists the tools wanted: alice changed the connections, and bob's read may come from a
 *  replica a moment behind. */
async function offers(page: Page, tools: Array<{ name: string; href: string }>): Promise<void> {
  await openUntil(page, "/", async () => {
    await openAccountMenu(page);
    await expect(page.getByRole("menu").getByRole("menuitem", { name: "Sign out" })).toBeVisible(ONE_LOOK);
    expect(await switchTo(page)).toEqual(tools);
  });
}

test.describe("switching to the other tools", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
    await api.DELETE("/stator/connection");
  });
  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
    await api.DELETE("/stator/connection");
  });

  test("a member finds each connected tool in the account menu, and none once they are removed", async ({ api, pageAs }) => {
    const page = await pageAs("bob");
    await openShell(page, "/");
    await offers(page, []);
    await expect(page.getByRole("menu").getByText("Switch to", { exact: true })).toHaveCount(0);

    expect((await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } })).response.status).toBe(200);
    expect((await api.PUT("/stator/connection", { body: { baseUrl: statorURL() } })).response.status).toBe(200);
    await offers(page, [
      { name: "Armature", href: `${armatureURL()}/` },
      { name: "Stator", href: `${statorURL()}/` },
    ]);
    await expect(page.getByRole("menu").getByRole("group", { name: "Switch to" })).toBeVisible();
    await expectAccessible(page);

    await api.DELETE("/stator/connection");
    await offers(page, [{ name: "Armature", href: `${armatureURL()}/` }]);

    await api.DELETE("/armature/connection");
    await offers(page, []);
    await expect(page.getByRole("menu").getByText("Switch to", { exact: true })).toHaveCount(0);
  });
});
