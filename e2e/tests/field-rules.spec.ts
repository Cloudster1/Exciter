import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Field rules (#206): alice makes Submit for review ask for a derives link to the requirement, so a submit without
// one is refused with what is missing, and taken once the link is there.

const KEY = uniqueKey("FRUL");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("field rules", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a transition asking for a link is refused without it, and taken once it is there", async ({ page, api }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Traced" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brake lights follow the pedal" } }));

    await page.goto(`/projects/${KEY}/settings/workflow`);
    await page.getByRole("button", { name: /Design Default/ }).click();
    await page.locator('[data-workflow-edge-label="Submit for review"]').click();
    await page.getByLabel("Add a rule").selectOption({ label: "Links of a type" });
    await page.getByLabel("Link type").selectOption({ label: "Derives" });
    await page.getByLabel("Direction").selectOption({ label: "To it" });
    await expectAccessible(page);
    await page.locator('[data-action="add-rule"]').click();
    await expect(page.locator('[data-rule="validator.links_exist"]')).toContainText("Derives");
    await page.locator('[data-action="save-workflow"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}/settings/workflow`);

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const bar = page.locator("[data-transition-bar]");
    await bar.getByRole("button", { name: "Submit for review" }).click();
    await expect(bar).toContainText(`${KEY}-R1 has no Derives link to it yet. Link it, then take this transition.`);
    await expectAccessible(page);

    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-R2` } }, body: { typeKey: "derives", otherKey: `${KEY}-R1` } }));
    await page.reload();
    await bar.getByRole("button", { name: "Submit for review" }).click();
    await expect(page.getByText("In review").first()).toBeVisible();
    await expect(bar).not.toContainText("has no Derives link");
  });
});
