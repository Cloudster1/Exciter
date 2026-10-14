import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Suspect links (#36): R2 derives from R1, R1 changes, and R2's link waits in the project's list until alice reads
// the change and confirms it.

const KEY = uniqueKey("SUSP");
const UPSTREAM = `${KEY}-R1`;
const DOWNSTREAM = `${KEY}-R2`;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("suspect links", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a change upstream turns the link suspect until alice reads it and confirms the link", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Suspected" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brakes engage within 100 ms" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title: "Brake lights follow the pedal" } }));
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: DOWNSTREAM } }, body: { typeKey: "derives", otherKey: UPSTREAM } }));
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: UPSTREAM } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );

    const links = page.getByRole("region", { name: "Trace links" });
    await openUntil(page, `/p/${KEY}/r/${DOWNSTREAM}`, () => expect(links.getByText("Suspect")).toBeVisible(ONE_LOOK));

    await page.goto(`/p/${KEY}/suspects`);
    const list = page.getByRole("list", { name: "Suspect links" });
    await expect(list.getByRole("listitem")).toHaveCount(1);
    await expect(list).toContainText(`${UPSTREAM}, revision 1 to 2`);
    await expectAccessible(page);
    await list.getByRole("button", { name: `Check: ${DOWNSTREAM} derives from ${UPSTREAM}` }).click();

    const dialog = page.getByRole("dialog", { name: `Check: ${DOWNSTREAM} derives from ${UPSTREAM}` });
    await expect(dialog.getByRole("region", { name: `${UPSTREAM}, revision 1 to 2` })).toContainText("Brakes engage within 80 ms");
    await expectAccessible(page);
    await dialog.getByLabel("Why the link still holds").fill("The lights still follow the faster brakes.");
    await dialog.getByRole("button", { name: "Confirm link" }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByText("No link waits to be checked.")).toBeVisible();

    await page.goto(`/p/${KEY}/r/${DOWNSTREAM}`);
    await expect(links.getByRole("link", { name: new RegExp(UPSTREAM) })).toBeVisible();
    await expect(links.getByText("Suspect")).toHaveCount(0);
  });
});
