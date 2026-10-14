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

  test("alice files an issue per picked requirement, and each requirement shows its issue", async ({ page }) => {
    await page.goto(`/p/${KEY}/spec`);
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

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Armature issues" });
    const link = panel.locator('[data-armature-relation="verifies"] [data-armature-link="CP-6"]');
    await expect(link.getByRole("link", { name: new RegExp(`${KEY}-R1 Brakes engage within 100 ms`) })).toBeVisible();
    await expect(link).toHaveAttribute("data-sync", "synced");
    const held = (await stubLinks(TENANT)).filter((l) => l.issueKey === "CP-6");
    expect(held).toHaveLength(1);
    expect(held[0]).toMatchObject({ title: `${KEY}-R1 Brakes engage within 100 ms (verifies)`, createdByName: "Alice" });
  });
});
