import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Issues made from requirements (#28): picked in the specification, filed in the stub as alice, each traced to
// its requirement and put on its issue by the stack's own worker.

const KEY = uniqueKey("ACR");
// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-armature-create";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;

interface StubLink {
  issueKey: string;
  title: string;
  createdByName: string;
}

async function stubLinks(tenant: string): Promise<StubLink[]> {
  const res = await fetch(`${armatureURL()}/_stub/${tenant}/remote-links`);
  return ((await res.json()) as { remoteLinks: StubLink[] }).remoteLinks;
}

test.describe("creating Armature issues from requirements", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(TENANT, "alice") } }));
    must(await api.POST("/projects", { body: { key: KEY, name: "Armature issues" } }));
    for (const title of ["Brakes engage within 100 ms", "Log every login"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
    }
  });

  test.afterEach(async ({ api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
    await api.DELETE("/armature/connection");
  });

  test("alice leaves a requirement out of the round, and clears a selection she does not want (#326)", async ({ page, api }) => {
    await page.goto(`/projects/${KEY}/spec`);
    const document = page.getByRole("article", { name: "Specification" });
    const bar = page.locator("[data-selection-bar]");
    await document.getByRole("checkbox", { name: `Select ${KEY}-R1` }).check();
    await document.getByRole("checkbox", { name: `Select ${KEY}-R2` }).check();
    await expect(bar.getByText("2 requirements selected")).toBeVisible();
    await bar.locator('[data-action="clear-selection"]').click();
    await expect(bar).toHaveCount(0);
    await expect(document.getByRole("checkbox", { name: `Select ${KEY}-R1` })).not.toBeChecked();
    await expect(document.getByRole("checkbox", { name: `Select ${KEY}-R2` })).not.toBeChecked();

    await document.getByRole("checkbox", { name: `Select ${KEY}-R1` }).check();
    await document.getByRole("checkbox", { name: `Select ${KEY}-R2` }).check();
    await page.getByRole("button", { name: "Create Armature issues" }).click();
    await expect(page.getByRole("dialog", { name: "Create 2 Armature issues" })).toBeVisible();
    // The title counts what is left in the round, so the dialog is found by its role alone.
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: `Leave out ${KEY}-R2` }).click();
    await expect(page.getByRole("dialog", { name: "Create an Armature issue" })).toBeVisible();
    await expect(dialog.locator(`[data-create-requirement="${KEY}-R2"]`)).toHaveCount(0);
    await expect(dialog.getByRole("button", { name: `Leave out ${KEY}-R1` })).toHaveCount(0);
    await dialog.getByRole("button", { name: "Create 1 issue" }).click();
    await expect(dialog.getByText("The issue was made.")).toBeVisible();
    await expect(dialog.locator("[data-result]")).toHaveCount(1);
    await expect(dialog.locator(`[data-requirement="${KEY}-R1"]`)).toHaveAttribute("data-result", "created");

    for (const [reqKey, count] of [
      [`${KEY}-R1`, 1],
      [`${KEY}-R2`, 0],
    ] as const) {
      const links = must(await api.GET("/requirements/{reqKey}/armature-links", { params: { path: { reqKey } } })).links;
      expect(links, `${reqKey}'s traces`).toHaveLength(count);
    }
  });

  test("a round Armature refuses halfway says which issue was made and why the other was not (#326)", async ({ page, api }) => {
    const refused = `${KEY}-R2 Log every login`;
    const steer = (summary: string) =>
      fetch(`${armatureURL()}/_stub/${TENANT}/refused-summary`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ summary }),
      });
    expect((await steer(refused)).status).toBe(204);
    try {
      await page.goto(`/projects/${KEY}/spec`);
      const document = page.getByRole("article", { name: "Specification" });
      await document.getByRole("checkbox", { name: `Select ${KEY}-R1` }).check();
      await document.getByRole("checkbox", { name: `Select ${KEY}-R2` }).check();
      await page.getByRole("button", { name: "Create Armature issues" }).click();
      const dialog = page.getByRole("dialog", { name: "Create 2 Armature issues" });
      await dialog.getByRole("button", { name: "Create 2 issues" }).click();

      await expect(dialog.getByText("1 of 2 issues were made.")).toBeVisible();
      await expect(dialog.locator(`[data-requirement="${KEY}-R1"]`)).toHaveAttribute("data-result", "created");
      const failed = dialog.locator(`[data-requirement="${KEY}-R2"]`);
      await expect(failed).toHaveAttribute("data-result", "failed");
      await expect(failed).toContainText("Armature refuses this summary.");
      await expectAccessible(page);
      const r2 = must(await api.GET("/requirements/{reqKey}/armature-links", { params: { path: { reqKey: `${KEY}-R2` } } })).links;
      expect(r2).toEqual([]);
    } finally {
      await steer("");
    }
  });

  test("alice files an issue per picked requirement, and each requirement shows its issue", async ({ page }) => {
    await page.goto(`/projects/${KEY}/spec`);
    const document = page.getByRole("article", { name: "Specification" });
    await document.getByRole("checkbox", { name: `Select ${KEY}-R1` }).check();
    await document.getByRole("checkbox", { name: `Select ${KEY}-R2` }).check();
    await expect(page.getByText("2 requirements selected")).toBeVisible();
    await page.getByRole("button", { name: "Create Armature issues" }).click();

    const dialog = page.getByRole("dialog", { name: "Create 2 Armature issues" });
    await expect(dialog.getByLabel("Armature project")).toHaveValue("CP");
    await dialog.getByLabel("Issue type").selectOption("Story");
    await dialog.getByLabel("The new issues").selectOption("verifies");
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Create 2 issues" }).click();

    await expect(dialog.getByText("2 of 2 issues were made.")).toBeVisible();
    await expect(dialog.getByRole("link", { name: "CP-6" })).toHaveAttribute("href", `${armatureURL()}/issues/CP-6`);
    await expect(dialog.getByRole("link", { name: "CP-7" })).toBeVisible();
    await dialog.getByRole("button", { name: "Done" }).click();
    await expect(page.getByText("2 requirements selected")).toBeHidden();

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Armature issues" });
    const link = panel.locator('[data-armature-relation="verifies"] [data-armature-link="CP-6"]');
    await expect(link.getByRole("link", { name: new RegExp(`${KEY}-R1 Brakes engage within 100 ms`) })).toBeVisible();
    await expect(link).toHaveAttribute("data-sync", "synced");
    const held = (await stubLinks(TENANT)).filter((l) => l.issueKey === "CP-6");
    expect(held).toHaveLength(1);
    expect(held[0]).toMatchObject({ title: `${KEY}-R1 Brakes engage within 100 ms (verifies)`, createdByName: "Alice" });
  });
});
