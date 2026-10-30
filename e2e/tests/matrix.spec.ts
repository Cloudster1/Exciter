import { readFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The matrix's axes (#147): alice counts only the verifying links, only those pointing at each row, and finds the
// same matrix after a reload and in its file. Its gaps (#148): what nothing links to, as rows and as columns. Its
// cells (#149): a gap closed and opened again where it shows. Its views (#152): saved, shared and opened by bob. Its
// workbook (#154).

const KEY = uniqueKey("MTRX");
// Tests run side by side, so each makes its own project.
const GAP_KEY = uniqueKey("MGAP");
const CELL_KEY = uniqueKey("MCEL");
const VIEW_KEY = uniqueKey("MVEW");

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("the traceability matrix", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api, KEY);
    await removeProject(api, GAP_KEY);
    await removeProject(api, CELL_KEY);
    await removeProject(api, VIEW_KEY);
  });

  test("alice picks the link types and the direction that count, and the address and the file keep them", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Matrix" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake pedal reads within 10 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    for (const [from, typeKey] of [
      ["R2", "derives"],
      ["R3", "verifies"],
    ] as const) {
      must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-${from}` } }, body: { typeKey, otherKey: `${KEY}-R1` } }));
    }

    await page.goto(`/projects/${KEY}/matrix`);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R2"]`)).toHaveAttribute("title", `${KEY}-R1 to ${KEY}-R2: Derives`);

    await page.getByRole("group", { name: "Link types" }).getByRole("button", { name: "Verifies" }).click();
    await expect(matrix.getByRole("columnheader", { name: `${KEY}-R2` })).toHaveCount(0);
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R3"]`)).toHaveAttribute("title", `${KEY}-R1 to ${KEY}-R3: Verifies`);
    await page.getByRole("button", { name: "Incoming" }).click();
    await expect(matrix.getByRole("columnheader", { name: `${KEY}-R1` })).toHaveCount(0);
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R3"]`)).toBeVisible();
    await expectAccessible(page);

    await page.reload();
    await expect(page.getByRole("group", { name: "Link types" }).getByRole("button", { name: "Verifies" })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("button", { name: "Incoming" })).toHaveAttribute("aria-pressed", "true");
    await expect(matrix.locator(`[data-cell="${KEY}-R1:${KEY}-R3"]`)).toBeVisible();
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download CSV" }).click();
    const file = await readFile((await (await download).path()) ?? "", "utf8");
    expect(file).toContain(`Key,Title,Links,${KEY}-R3`);
    expect(file).not.toContain(`,${KEY}-R2`);

    // The same matrix as an Excel workbook (#154), a zip of its parts.
    const workbook = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download Excel" }).click();
    const saved = await workbook;
    expect(saved.suggestedFilename()).toBe(`${KEY}-matrix.xlsx`);
    const bytes = await readFile((await saved.path()) ?? "");
    expect(bytes.subarray(0, 2).toString()).toBe("PK");
  });

  test("alice finds what nothing links to, in the rows and in the columns of a column filter", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: GAP_KEY } } };
    must(await api.POST("/projects", { body: { key: GAP_KEY, name: "Gaps" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake pedal reads within 10 ms", "Horn sounds"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${GAP_KEY}-R2` } },
        body: { typeKey: "derives", otherKey: `${GAP_KEY}-R1` },
      }),
    );

    await page.goto(`/projects/${GAP_KEY}/matrix`);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.locator(`[data-cell="${GAP_KEY}-R1:${GAP_KEY}-R2"]`)).toBeVisible();
    await page.getByLabel("Only gaps").selectOption("rows");
    await expect(matrix.getByRole("rowheader", { name: new RegExp(`${GAP_KEY}-R3`) })).toBeVisible();
    await expect(matrix.getByRole("rowheader", { name: new RegExp(`${GAP_KEY}-R1 `) })).toHaveCount(0);

    await page.getByRole("combobox", { name: "Column query" }).fill("kind = requirement");
    await page.getByRole("combobox", { name: "Column query" }).press("Enter");
    await page.getByLabel("Only gaps").selectOption("columns");
    await expect(matrix.getByRole("columnheader", { name: `${GAP_KEY}-R3` })).toBeVisible();
    await expect(matrix.getByRole("columnheader", { name: `${GAP_KEY}-R1` })).toHaveCount(0);
    await expect(matrix.locator("[data-gap]").first()).toBeVisible();
    await expectAccessible(page);
  });

  test("alice closes a gap from its cell, and opens it again", async ({ page, api }) => {
    const projectKey = { params: { path: { projectKey: CELL_KEY } } };
    must(await api.POST("/projects", { body: { key: CELL_KEY, name: "Cells" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }

    await page.goto(`/projects/${CELL_KEY}/matrix?columnQuery=${encodeURIComponent("kind = requirement")}`);
    await page.getByRole("button", { name: `Links between ${CELL_KEY}-R2 and ${CELL_KEY}-R1` }).click();
    const panel = page.getByRole("dialog", { name: `${CELL_KEY}-R2 and ${CELL_KEY}-R1` });
    await expect(panel.getByText("No link between these yet.")).toBeVisible();
    await panel.getByLabel("Link type").selectOption({ label: "This requirement verifies" });
    await panel.getByRole("button", { name: "Link them" }).click();
    await expect(panel.getByText(`Verifies ${CELL_KEY}-R1`, { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: `${CELL_KEY}-R2 to ${CELL_KEY}-R1: Verifies` })).toBeVisible();
    await expectAccessible(page);

    await panel.getByRole("button", { name: `Remove the link verifies ${CELL_KEY}-R1` }).click();
    await expect(panel.getByText("No link between these yet.")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(panel).toHaveCount(0);
    await expect(page.locator(`[data-cell="${CELL_KEY}-R2:${CELL_KEY}-R1"] [data-gap]`)).toHaveCount(0);
    await expect(page.getByRole("button", { name: `Links between ${CELL_KEY}-R2 and ${CELL_KEY}-R1` })).toBeVisible();
  });

  test("alice shares the matrix as she set it, and bob opens it so", async ({ page, api, pageAs }) => {
    const projectKey = { params: { path: { projectKey: VIEW_KEY } } };
    must(await api.POST("/projects", { body: { key: VIEW_KEY, name: "Views" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title } }));
    }
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${VIEW_KEY}-R2` } },
        body: { typeKey: "verifies", otherKey: `${VIEW_KEY}-R1` },
      }),
    );

    await page.goto(`/projects/${VIEW_KEY}/matrix`);
    await page.getByRole("group", { name: "Link types" }).getByRole("button", { name: "Verifies" }).click();
    await page.getByRole("button", { name: "Incoming" }).click();
    await page.getByRole("button", { name: "Save as a view" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name").fill("Verified");
    await dialog.getByLabel("Share with everybody in the project").check();
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Saved the view Verified." })).toBeVisible();

    const bob = await pageAs("bob");
    await bob.goto(`/projects/${VIEW_KEY}/matrix`);
    await bob.getByLabel("Views").selectOption({ label: "Verified (Shared)" });
    await expect(bob.getByRole("group", { name: "Link types" }).getByRole("button", { name: "Verifies" })).toHaveAttribute("aria-pressed", "true");
    await expect(bob.getByRole("button", { name: "Incoming" })).toHaveAttribute("aria-pressed", "true");
    await expect(bob.locator(`[data-cell="${VIEW_KEY}-R1:${VIEW_KEY}-R2"]`)).toHaveAttribute("title", `${VIEW_KEY}-R1 to ${VIEW_KEY}-R2: Verifies`);
    await expectAccessible(bob);
  });
});
