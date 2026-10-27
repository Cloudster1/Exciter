import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Armature issues as the matrix's columns (#315): the issues the rows are traced to, narrowed by relation, and a
// row traced to one more from its cell.

const KEY = uniqueKey("MISS");
// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-matrix-issues";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;

test.describe("Armature issues as the matrix's columns", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(TENANT, "alice") } }));
    must(await api.POST("/projects", { body: { key: KEY, name: "Matrix issues" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake lights follow the pedal"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
    }
    for (const [req, issueKey, relation] of [
      ["R1", "CP-1", "implements"],
      ["R2", "CP-2", "verifies"],
    ] as const) {
      must(await api.POST("/requirements/{reqKey}/armature-links", { params: { path: { reqKey: `${KEY}-${req}` } }, body: { issueKey, relation } }));
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

  test("alice shows the issues the rows are traced to, narrows them by relation, and traces a row to one from its cell", async ({ page }) => {
    await page.goto(`/p/${KEY}/matrix`);
    await page.getByRole("group", { name: "Columns" }).getByRole("button", { name: "Armature issues" }).click();
    await expect(page).toHaveURL(/columns=issues/);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.getByRole("columnheader", { name: "CP-1" })).toBeVisible();
    await expect(matrix.getByRole("columnheader", { name: "CP-2" })).toBeVisible();
    await expect(matrix.locator(`[data-cell="${KEY}-R1:CP-1"]`)).toHaveAttribute("title", `${KEY}-R1 to CP-1: implements`);
    await expectAccessible(page);

    const relations = page.getByRole("group", { name: "Relations" });
    await relations.getByRole("button", { name: "Verified by" }).click();
    await expect(matrix.getByRole("columnheader", { name: "CP-1" })).toHaveCount(0);
    await expect(matrix.locator(`[data-cell="${KEY}-R2:CP-2"]`)).toHaveAttribute("title", `${KEY}-R2 to CP-2: verifies`);
    await relations.getByRole("button", { name: "Verified by" }).click();
    await expect(matrix.getByRole("columnheader", { name: "CP-1" })).toBeVisible();

    await page.getByRole("button", { name: `Links between ${KEY}-R1 and CP-2` }).click();
    const panel = page.getByRole("dialog", { name: `${KEY}-R1 and CP-2` });
    await expect(panel.getByText("No link between these yet.")).toBeVisible();
    await panel.getByLabel("Relation").selectOption({ label: "Verified by" });
    await panel.getByRole("button", { name: "Link them" }).click();
    await expect(panel.getByText("Verified by CP-2", { exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(matrix.locator(`[data-cell="${KEY}-R1:CP-2"]`)).toHaveAttribute("title", `${KEY}-R1 to CP-2: verifies`);
  });
});
