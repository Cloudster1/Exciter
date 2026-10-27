import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Reuse and variants (#49): alice reuses a library's requirement in her product, the library revises it, and the
// product takes the new revision.

const LIB = uniqueKey("LIB");
const APP = uniqueKey("APP");
// Low enough that a draft with the library requirement's very title is found however the trigrams weigh it.
const SIMILAR = 0.4;

async function libraryAndProduct(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: LIB, name: "Brake library" } }));
  must(await api.POST("/projects", { body: { key: APP, name: "Scooter" } }));
  must(await api.PATCH("/projects/{projectKey}", { params: { path: { projectKey: LIB } }, body: { settings: { library: true } } }));
}

async function cleanUp(api: ExciterApi) {
  for (const key of [APP, LIB]) {
    const path = { params: { path: { projectKey: key } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (!found.data) continue;
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

test.describe("reuse and variants", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice reuses a library requirement in her product, and takes its new revision", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: LIB, name: "Brake library" } }));
    must(await api.POST("/projects", { body: { key: APP, name: "Scooter" } }));
    must(await api.PATCH("/projects/{projectKey}", { params: { path: { projectKey: LIB } }, body: { settings: { library: true } } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: LIB } }, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(`/p/${LIB}/r/${LIB}-R1`);
    await page.locator('[data-action="reuse-requirement"]').click();
    const dialog = page.getByRole("dialog", { name: `Reuse ${LIB}-R1` });
    await dialog.getByLabel("Project").selectOption(APP);
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Reuse", exact: true }).click();

    await expect(page).toHaveURL(new RegExp(`/p/${APP}/r/${APP}-R1$`));
    const banner = page.locator("[data-reuse]");
    await expect(banner).toContainText(`Follows ${LIB}-R1 from the library, at its revision 1.`);
    await page.locator('[data-action="edit-requirement"]').click();
    await expect(page.locator("[data-reuse-locked]")).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${LIB}-R1` } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );
    await page.reload();
    await expect(banner).toHaveAttribute("data-reuse-behind", "true");
    await expect(banner).toContainText(`${LIB}-R1 has revision 2 now.`);
    await expectAccessible(page);
    await page.locator('[data-action="take-source"]').click();
    await expect(page.locator("[data-requirement-notice]")).toHaveText("Took revision 2 of the source.");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Brakes engage within 80 ms");
    await expect(banner).toHaveAttribute("data-reuse-behind", "false");
  });

  test("alice makes a variant with words of its own, then follows the source again", async ({ page, api }) => {
    await libraryAndProduct(api);
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: LIB } }, body: { title: "Brakes engage within 100 ms" } }));
    must(await api.POST("/requirements/{reqKey}/reuse", { params: { path: { reqKey: `${LIB}-R1` } }, body: { projectKey: APP, children: false, place: { parentId: null, afterId: null } } }));

    await page.goto(`/p/${APP}/r/${APP}-R1`);
    const banner = page.locator("[data-reuse]");
    await page.locator('[data-action="make-variant"]').click();
    await expect(page.locator("[data-requirement-notice]")).toHaveText("This is a variant now. Its words are its own.");
    await expect(banner).toHaveAttribute("data-reuse", "variant");
    await expect(banner).toContainText(`A variant of ${LIB}-R1 from the library, branched at its revision 1.`);
    await page.locator('[data-action="edit-requirement"]').click();
    await page.getByLabel("Title", { exact: true }).fill("Brakes engage within 60 ms on the scooter");
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Brakes engage within 60 ms on the scooter");

    must(
      await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: `${LIB}-R1` } }, body: { title: "Brakes engage within 80 ms", baseRevision: 1 } }),
    );
    await page.reload();
    const take = page.locator('[data-action="take-source"]');
    await expect(take).toHaveText("Take revision 2 and follow again");
    await expectAccessible(page);
    await take.click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Brakes engage within 80 ms");
    await expect(banner).toHaveAttribute("data-reuse", "following");
    await expect(banner).toContainText(`Follows ${LIB}-R1 from the library, at its revision 2.`);
  });

  test("alice reuses the library's requirement her new one is like instead of writing it again", async ({ page, api }) => {
    await libraryAndProduct(api);
    must(await api.PUT("/projects/{projectKey}/duplicate-settings", { params: { path: { projectKey: APP } }, body: { threshold: SIMILAR } }));
    // A title no other spec writes, so another library's requirement is never the one found.
    const title = `Regenerative braking recovers energy downhill ${LIB}`;
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: LIB } }, body: { title } }));

    await page.goto(`/p/${APP}/new`);
    await page.getByLabel("Title", { exact: true }).fill(title);
    const hit = page.locator(`[data-similar-hint] [data-similar="${LIB}-R1"][data-library]`);
    await expect(hit).toBeVisible();
    await expectAccessible(page);
    await hit.locator('[data-action="reuse-similar"]').click();
    const dialog = page.getByRole("dialog", { name: `Reuse ${LIB}-R1` });
    await expect(dialog.getByLabel("Project")).toHaveValue(APP);
    await dialog.locator('[data-action="confirm-reuse"]').click();

    await expect(page).toHaveURL(new RegExp(`/p/${APP}/r/${APP}-R1$`));
    await expect(page.locator("[data-reuse]")).toContainText(`Follows ${LIB}-R1 from the library, at its revision 1.`);
  });

  test("alice reuses a section with everything below it, and each arrives reused", async ({ page, api }) => {
    await libraryAndProduct(api);
    const lib = { params: { path: { projectKey: LIB } } };
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...lib, body: { kind: "section", title: "Braking" } })).requirement;
    const first = must(
      await api.POST("/projects/{projectKey}/requirements", { ...lib, body: { title: "Brakes engage within 100 ms", place: { parentId: section.id, afterId: null } } }),
    ).requirement;
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...lib,
        body: { title: "Brakes hold on a slope", place: { parentId: section.id, afterId: first.id } },
      }),
    );

    await page.goto(`/p/${LIB}/r/${section.key}`);
    await page.locator('[data-action="reuse-requirement"]').click();
    const dialog = page.getByRole("dialog", { name: `Reuse ${section.key}` });
    await dialog.getByLabel("Project").selectOption(APP);
    await expect(dialog.getByRole("checkbox", { name: "Reuse everything below it too" })).toBeChecked();
    await dialog.locator('[data-action="confirm-reuse"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${APP}/r/${APP}-R1$`));

    await page.goto(`/p/${APP}/spec`);
    for (const key of [`${APP}-R1`, `${APP}-R2`, `${APP}-R3`]) {
      await expect(page.locator(`[data-spec-entry="${key}"] [data-spec-reuse="following"]`)).toHaveText("Reused");
    }
    await expect(page.locator(`[data-spec-entry="${APP}-R3"]`)).toContainText("Brakes hold on a slope");
    await expectAccessible(page);
  });
});
