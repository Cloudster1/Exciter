import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Baselines (#37): alice takes a baseline, R1 changes and R3 is written, and comparing the baseline with now says so
// before she locks it.

const KEY = uniqueKey("BASE");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("baselines", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice takes a baseline, compares it with now after a change, and locks it", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Baselined" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brakes engage within 100 ms" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brake lights follow the pedal" } }));

    await page.goto(`/p/${KEY}/baselines`);
    await page.getByRole("button", { name: "Take baseline" }).click();
    const form = page.getByRole("form", { name: "Take a baseline" });
    await form.getByLabel("Name").fill("Release 1.0");
    await expectAccessible(page);
    await form.getByRole("button", { name: "Take" }).click();
    await expect(page.getByText("The baseline Release 1.0 is taken.")).toBeVisible();

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${KEY}-R1` } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brake fluid is checked" } }));

    await page.getByRole("list", { name: "Baselines" }).getByRole("link", { name: "Release 1.0" }).click();
    const spec = page.getByRole("list", { name: "Release 1.0" });
    await expect(spec.getByRole("listitem")).toHaveCount(2);
    await expect(spec).toContainText("Brakes engage within 100 ms");
    await page.getByRole("button", { name: "Compare" }).click();

    const items = page.getByRole("list", { name: "Requirements" });
    await expect(items.locator(`[data-item-change="${KEY}-R1"]`)).toContainText("Revision 1 to 2");
    await expect(items.locator(`[data-item-change="${KEY}-R3"]`)).toContainText("Added");
    await expectAccessible(page);
    await items.locator(`[data-item-change="${KEY}-R1"]`).getByRole("link", { name: "Show the change" }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${KEY}/r/${KEY}-R1/history\\?from=1&to=2`));

    await page.goBack();
    await page.getByRole("link", { name: "Release 1.0" }).click();
    page.once("dialog", (dialog) => void dialog.accept());
    await page.getByRole("button", { name: "Lock" }).click();
    await expect(page.getByText("A locked baseline keeps its name and is never deleted.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Delete" })).toHaveCount(0);
  });
});
