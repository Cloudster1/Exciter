import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Signed baselines and locked requirements (#205): alice names bob who signs, bob signs a baseline with his password,
// and a requirement moved into a status that locks offers no edit and refuses one.

const SIGN_KEY = uniqueKey("BSGN");
const LOCK_KEY = uniqueKey("LOCK");

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("control", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api, SIGN_KEY);
    await removeProject(api, LOCK_KEY);
  });

  test("alice names bob who signs, and bob signs a baseline with his password", async ({ page, api, pageAs, people }) => {
    const project = { params: { path: { projectKey: SIGN_KEY } } };
    must(await api.POST("/projects", { body: { key: SIGN_KEY, name: "Released" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(`/projects/${SIGN_KEY}/settings/approval`);
    const rule = page.locator("[data-signing-rule]");
    await rule.getByRole("button", { name: "Name who signs" }).click();
    await rule.getByRole("combobox", { name: "Add a person or group" }).pressSequentially(people.bob.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await expectAccessible(page);
    await rule.getByRole("button", { name: "Save" }).click();
    await expect(rule).toContainText(`1 signature of ${people.bob.name}`);
    const taken = must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 1.0" } })).baseline;

    const bob = await pageAs("bob");
    const signing = bob.locator("[data-baseline-signing]");
    await openUntil(bob, `/projects/${SIGN_KEY}/baselines/${taken.id}`, () => expect(signing.getByRole("button", { name: "Sign" })).toBeVisible(ONE_LOOK));
    await signing.getByRole("button", { name: "Sign" }).click();
    const dialog = bob.getByRole("dialog", { name: "Sign the baseline" });
    await expect(dialog).toContainText(`I release the baseline Release 1.0 of ${SIGN_KEY} as the agreed state of its requirements.`);
    await expectAccessible(bob);
    await dialog.getByLabel("Your password").fill("not my password");
    await dialog.getByRole("button", { name: "Sign and release" }).click();
    await expect(dialog).toContainText("That is not your password. Type it again to sign.");
    await dialog.getByLabel("Your password").fill(people.bob.password);
    await dialog.getByRole("button", { name: "Sign and release" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(signing).toContainText("Signed, with 1 of 1 signatures");
    await expect(bob.getByRole("button", { name: "Rename" })).toHaveCount(0);

    await expect
      .poll(async () => must(await api.GET("/baselines/{baselineID}", { params: { path: { baselineID: taken.id } } })).baseline.signing?.signed)
      .toBe(true);
  });

  test("a requirement moved into a status that locks offers no edit and refuses one", async ({ page, api }) => {
    const project = { params: { path: { projectKey: LOCK_KEY } } };
    must(await api.POST("/projects", { body: { key: LOCK_KEY, name: "Locked" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(`/projects/${LOCK_KEY}/settings/workflow`);
    const row = page.locator('[data-status-row="In review"]');
    await row.getByRole("button", { name: "Edit" }).click();
    await page.getByLabel("Locks its requirements").check();
    await page.getByRole("button", { name: "Save status" }).click();
    await expect(page.locator('[data-status-row="In review"] [data-status-locks]')).toBeVisible();

    await page.goto(`/projects/${LOCK_KEY}/r/${LOCK_KEY}-R1`);
    await expect(page.getByRole("button", { name: "Edit" })).toBeVisible();
    await page.getByRole("button", { name: "Submit for review" }).click();
    await expect(page.getByText(`${LOCK_KEY}-R1 is locked in the status In review. Take it out of In review to change it.`)).toBeVisible();
    await expect(page.getByRole("button", { name: "Edit" })).toHaveCount(0);
    await expectAccessible(page);

    const refused = await api.PATCH("/requirements/{reqKey}", {
      params: { path: { reqKey: `${LOCK_KEY}-R1` } },
      body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
    });
    expect(refused.response.status).toBe(409);
    expect(refused.error).toMatchObject({ error: { code: "requirement_locked" } });
  });
});
