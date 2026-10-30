import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The matrix walked further (#315): columns from another project, rows and columns a page at a time, a suspect
// link checked from its cell, and the way to the chains and back.

const ROWS_KEY = uniqueKey("MWRO");
const COLUMNS_KEY = uniqueKey("MWCO");
const PAGED_KEY = uniqueKey("MWPG");
const SUSPECT_KEY = uniqueKey("MWSU");
// One more than a page of rows and of columns, MATRIX_PAGE_SIZE and MATRIX_COLUMN_PAGE_SIZE in web/src/config.ts.
const PAGED_REQUIREMENTS = 101;
// Written one after another, since rows and columns follow the outline the writes leave, which with a page of a
// hundred columns by fifty rows takes longer than a test is given by default.
const PAGED_TIMEOUT_MS = 60_000;
// A page of fifty rows by a hundred columns is five thousand cells: on a busy shard they render for longer after the
// page arrives than the five seconds an expectation waits by default.
const PAGE_SHOWN = { timeout: 20_000 };

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function write(api: ExciterApi, projectKey: string, titles: string[]) {
  for (const title of titles) must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey } }, body: { title } }));
}

test.describe("walking the traceability matrix", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    for (const key of [ROWS_KEY, COLUMNS_KEY, PAGED_KEY, SUSPECT_KEY]) await removeProject(api, key);
  });

  test("alice lays the requirements of another project out as the columns", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: ROWS_KEY, name: "Brake rows" } }));
    must(await api.POST("/projects", { body: { key: COLUMNS_KEY, name: "Caliper columns" } }));
    await write(api, ROWS_KEY, ["Brakes engage within 100 ms", "Brake lights follow the pedal"]);
    await write(api, COLUMNS_KEY, ["Caliper clamps within 80 ms"]);
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${ROWS_KEY}-R1` } },
        body: { typeKey: "derives", otherKey: `${COLUMNS_KEY}-R1` },
      }),
    );
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${ROWS_KEY}-R2` } },
        body: { typeKey: "relates", otherKey: `${ROWS_KEY}-R1` },
      }),
    );

    await page.goto(`/projects/${ROWS_KEY}/matrix`);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.getByRole("columnheader", { name: `${ROWS_KEY}-R2` })).toBeVisible();
    await page.getByLabel("Columns from").selectOption({ label: "Caliper columns" });
    await expect(page).toHaveURL(new RegExp(`columnProject=${COLUMNS_KEY}`));
    await expect(matrix.locator(`[data-cell="${ROWS_KEY}-R1:${COLUMNS_KEY}-R1"]`)).toHaveAttribute("title", `${ROWS_KEY}-R1 to ${COLUMNS_KEY}-R1: Derives`);
    await expect(matrix.getByRole("columnheader", { name: `${ROWS_KEY}-R2` })).toHaveCount(0);
    await expectAccessible(page);

    await page.reload();
    await expect(page.getByLabel("Columns from")).toHaveValue(COLUMNS_KEY);
    await expect(matrix.getByRole("columnheader", { name: `${COLUMNS_KEY}-R1` })).toBeVisible();
  });

  test("alice pages the rows and the columns of a column filter, and the address stays", async ({ page, api }) => {
    test.setTimeout(PAGED_TIMEOUT_MS);
    must(await api.POST("/projects", { body: { key: PAGED_KEY, name: "Paged" } }));
    await write(
      api,
      PAGED_KEY,
      Array.from({ length: PAGED_REQUIREMENTS }, (_, i) => `Brake check ${i + 1}`),
    );

    await page.goto(`/projects/${PAGED_KEY}/matrix?columnQuery=${encodeURIComponent("kind = requirement")}`);
    const address = page.url();
    const matrix = page.getByRole("table", { name: "Matrix" });
    const rows = page.getByRole("navigation", { name: "Matrix" });
    const columns = page.getByRole("navigation", { name: "Columns" });
    await expect(rows).toContainText(`1 to 50 of ${PAGED_REQUIREMENTS}`, PAGE_SHOWN);
    await expect(columns).toContainText(`1 to 100 of ${PAGED_REQUIREMENTS}`);
    await expect(rows.getByRole("button", { name: "Previous" })).toBeDisabled();

    await rows.getByRole("button", { name: "Next" }).click();
    await expect(rows).toContainText(`51 to 100 of ${PAGED_REQUIREMENTS}`);
    await expect(matrix.getByRole("rowheader", { name: new RegExp(`${PAGED_KEY}-R51 `) })).toBeVisible(PAGE_SHOWN);
    await expect(matrix.getByRole("rowheader", { name: new RegExp(`${PAGED_KEY}-R1 `) })).toHaveCount(0);
    await rows.getByRole("button", { name: "Next" }).click();
    await expect(rows).toContainText(`101 to 101 of ${PAGED_REQUIREMENTS}`, PAGE_SHOWN);
    await expect(rows.getByRole("button", { name: "Next" })).toBeDisabled();
    await rows.getByRole("button", { name: "Previous" }).click();
    await expect(rows).toContainText(`51 to 100 of ${PAGED_REQUIREMENTS}`, PAGE_SHOWN);

    await columns.getByRole("button", { name: "Next" }).click();
    await expect(columns).toContainText(`101 to 101 of ${PAGED_REQUIREMENTS}`);
    await expect(matrix.getByRole("columnheader", { name: `${PAGED_KEY}-R101` })).toBeVisible(PAGE_SHOWN);
    await expect(matrix.getByRole("columnheader", { name: `${PAGED_KEY}-R1`, exact: true })).toHaveCount(0);
    await columns.getByRole("button", { name: "Previous" }).click();
    await expect(matrix.getByRole("columnheader", { name: `${PAGED_KEY}-R1`, exact: true })).toBeVisible(PAGE_SHOWN);
    expect(page.url()).toBe(address);
  });

  test("alice checks a suspect link from its cell, and moves to the chains and back", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: SUSPECT_KEY, name: "Suspect" } }));
    await write(api, SUSPECT_KEY, ["Brakes engage within 100 ms", "Brake lights follow the pedal"]);
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${SUSPECT_KEY}-R2` } },
        body: { typeKey: "derives", otherKey: `${SUSPECT_KEY}-R1` },
      }),
    );
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${SUSPECT_KEY}-R1` } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );

    await page.goto(`/projects/${SUSPECT_KEY}/matrix`);
    await page.getByRole("button", { name: `${SUSPECT_KEY}-R1 to ${SUSPECT_KEY}-R2: Derives (suspect)` }).click();
    const panel = page.getByRole("dialog", { name: `${SUSPECT_KEY}-R1 and ${SUSPECT_KEY}-R2` });
    await expect(panel.getByText("Suspect", { exact: true })).toBeVisible();
    await panel.getByRole("button", { name: `Check the link to ${SUSPECT_KEY}-R2` }).click();
    const dialog = page.getByRole("dialog", { name: `Check: ${SUSPECT_KEY}-R2 derives from ${SUSPECT_KEY}-R1` });
    await expect(dialog.getByRole("region", { name: `${SUSPECT_KEY}-R1, revision 1 to 2` })).toBeVisible();
    await expectAccessible(page);
    await dialog.getByLabel("Why the link still holds").fill("The lights still follow the faster brakes.");
    await dialog.getByRole("button", { name: "Confirm link" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(panel.getByText("Suspect", { exact: true })).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("button", { name: `${SUSPECT_KEY}-R1 to ${SUSPECT_KEY}-R2: Derives`, exact: true })).toBeVisible();

    await page.locator('[data-action="open-chains"]').click();
    await expect(page).toHaveURL(new RegExp(`/projects/${SUSPECT_KEY}/chains$`));
    await expect(page.getByRole("heading", { name: "Chains", level: 1 })).toBeVisible();
    await page.locator('[data-action="open-matrix"]').click();
    await expect(page).toHaveURL(new RegExp(`/projects/${SUSPECT_KEY}/matrix$`));
    await expect(page.getByRole("button", { name: `${SUSPECT_KEY}-R1 to ${SUSPECT_KEY}-R2: Derives`, exact: true })).toBeVisible();
  });
});
