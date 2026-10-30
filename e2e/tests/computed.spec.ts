import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Computed attributes (#213): alice defines a roll-up of a section's children with a formula, and watches it change
// as a child's cost does.

const KEY = uniqueKey("CMPT");
// The worker works a value out a moment after a change; this is how long the page is given to show it.
const WORKED_OUT_MS = 30_000;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("computed attributes", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice defines a roll-up and watches it change", async ({ page, api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Rolled up" } }));
    const cost = must(await api.POST("/projects/{projectKey}/attributes", { ...path, body: { name: "Cost", kind: "number" } })).attribute;
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Brakes", kind: "section" } })).requirement;
    const child = must(
      await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Pedal travel", attributes: { [cost.id]: 10 } } }),
    ).requirement;
    must(
      await api.POST("/requirements/{reqKey}/move", {
        params: { path: { reqKey: child.key } },
        body: { place: { parentId: section.id, afterId: null } },
      }),
    );

    await page.goto(`/projects/${KEY}/settings/attributes`);
    await page.getByRole("button", { name: "New attribute" }).click();
    const form = page.locator("[data-attribute-form]");
    await form.getByLabel("Name", { exact: true }).fill("Total cost");
    await form.getByLabel("Work it out from a formula").check();
    await form.getByLabel("Formula", { exact: true }).fill(`sum(children, "Cost")`);
    await expect(form.getByText("Gives a number. Reads Cost")).toBeVisible();
    await form.getByLabel("Work it out for sections too").check();
    await expectAccessible(page);
    await form.getByRole("button", { name: "New attribute" }).click();
    await expect(page.locator('[data-attribute="Total cost"]').getByText("Computed")).toBeVisible();

    await page.goto(`/projects/${KEY}/r/${section.key}`);
    const total = page.locator('[data-attribute-value="Total cost"]');
    await expect(async () => {
      await page.reload();
      await expect(total).toContainText("10", { timeout: 1000 });
    }).toPass({ timeout: WORKED_OUT_MS });
    await expectAccessible(page);

    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: child.key } }, body: { attributes: { [cost.id]: 25 }, baseRevision: 1 } }));
    await expect(async () => {
      await page.reload();
      await expect(total).toContainText("25", { timeout: 1000 });
    }).toPass({ timeout: WORKED_OUT_MS });
  });
});
