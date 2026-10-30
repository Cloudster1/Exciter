import { readFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, confirm, toast, dismissToasts } from "../fixtures/shell";
import { unzipText } from "../fixtures/unzip";

// The baseline controls baselines.spec.ts and control.spec.ts leave alone (#316): a rename and a delete, a baseline
// of a section, two baselines compared, the matrix as a workbook, and who signs changed, signed twice and stopped.

const RENAME_KEY = uniqueKey("BREN");
const SECTION_KEY = uniqueKey("BSEC");
const XLSX_KEY = uniqueKey("BXLS");
const SIGN_KEY = uniqueKey("BSGT");
// The spec has alice and bob, so the group that signs holds alice, who signs through it.
const GROUP = `Release board ${SIGN_KEY}`;

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("baseline controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    for (const key of [RENAME_KEY, SECTION_KEY, XLSX_KEY, SIGN_KEY]) await removeProject(api, key);
    await withDatabase(async (db) => void (await db.query("DELETE FROM groups WHERE name = $1", [GROUP])));
  });

  test("alice renames a baseline and deletes it", async ({ page, api }) => {
    const project = { params: { path: { projectKey: RENAME_KEY } } };
    must(await api.POST("/projects", { body: { key: RENAME_KEY, name: "Renamed" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    const taken = must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 1.0" } })).baseline;

    await page.goto(`/p/${RENAME_KEY}/baselines/${taken.id}`);
    await page.locator('[data-action="rename-baseline"]').click();
    const form = page.getByRole("form", { name: "Rename" });
    await form.getByLabel("Name").fill("Release 1.0 final");
    await expectAccessible(page);
    await form.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("heading", { name: "Release 1.0 final" })).toBeVisible();
    await page.getByRole("link", { name: "All baselines" }).click();
    const list = page.getByRole("list", { name: "Baselines" });
    await expect(list.getByRole("link", { name: "Release 1.0 final" })).toBeVisible();
    await expect(list.getByRole("link", { name: "Release 1.0", exact: true })).toHaveCount(0);

    await list.getByRole("link", { name: "Release 1.0 final" }).click();
    await page.locator('[data-action="delete-baseline"]').click();
    await confirm(page);
    await expect(page).toHaveURL(new RegExp(`/p/${RENAME_KEY}/baselines$`));
    await expect(toast(page, "The baseline Release 1.0 final is deleted.")).toBeVisible();
    await dismissToasts(page);
    await expect(page.getByText("No baseline yet.")).toBeVisible();
    const gone = await api.GET("/baselines/{baselineID}", { params: { path: { baselineID: taken.id } } });
    expect(gone.response.status).toBe(404);
  });

  test("alice takes a baseline of a section, and compares two baselines of the project", async ({ page, api }) => {
    const project = { params: { path: { projectKey: SECTION_KEY } } };
    must(await api.POST("/projects", { body: { key: SECTION_KEY, name: "Sections" } }));
    const section = must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { kind: "section", title: "Braking" } })).requirement;
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...project,
        body: { title: "Brakes engage within 100 ms", place: { parentId: section.id, afterId: null } },
      }),
    );
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Lights follow the pedal" } }));

    await page.goto(`/p/${SECTION_KEY}/baselines`);
    await page.getByRole("button", { name: "Take baseline" }).click();
    const form = page.getByRole("form", { name: "Take a baseline" });
    await form.getByLabel("Name").fill("Braking 1.0");
    await form.getByLabel("Of", { exact: true }).selectOption({ label: "1 Braking" });
    await form.getByRole("button", { name: "Take" }).click();
    await expect(page.getByText("The baseline Braking 1.0 is taken.")).toBeVisible();
    await expect(page.locator('[data-baseline="Braking 1.0"]')).toContainText(`Of ${SECTION_KEY}-R1 Braking`);
    await page.getByRole("list", { name: "Baselines" }).getByRole("link", { name: "Braking 1.0" }).click();
    const held = page.getByRole("list", { name: "Braking 1.0" });
    await expect(held.locator("[data-baseline-entry]")).toHaveCount(2);
    await expect(held.locator(`[data-baseline-entry="${SECTION_KEY}-R2"]`)).toBeVisible();
    await expect(held.locator(`[data-baseline-entry="${SECTION_KEY}-R3"]`)).toHaveCount(0);

    const before = must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 1" } })).baseline;
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${SECTION_KEY}-R2` } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );
    expect((await api.DELETE("/requirements/{reqKey}", { params: { path: { reqKey: `${SECTION_KEY}-R3` } } })).response.status).toBe(204);
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brake fluid is checked" } }));
    must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 2" } }));

    await page.goto(`/p/${SECTION_KEY}/baselines/${before.id}`);
    await page.getByLabel("Compare with").selectOption({ label: "Release 2" });
    await page.getByRole("button", { name: "Compare" }).click();
    await expect(page.getByRole("heading", { name: "Release 1 compared with Release 2" })).toBeVisible();
    const items = page.getByRole("list", { name: "Requirements" });
    await expect(items.locator("[data-item-change]")).toHaveCount(3);
    await expect(items.locator(`[data-item-change="${SECTION_KEY}-R2"]`)).toHaveAttribute("data-change", "changed");
    await expect(items.locator(`[data-item-change="${SECTION_KEY}-R2"]`)).toContainText("Revision 1 to 2");
    await expect(items.locator(`[data-item-change="${SECTION_KEY}-R3"]`)).toHaveAttribute("data-change", "removed");
    await expect(items.locator(`[data-item-change="${SECTION_KEY}-R4"]`)).toHaveAttribute("data-change", "added");
    await expectAccessible(page);

    // The print view the PDF is made of holds what the baseline held when taken, not what the project reads now.
    await page.goto(`/print/baselines/${before.id}`);
    await expect(page.locator("html[data-print-ready]")).toBeAttached();
    const printed = page.locator('[data-print-view] [data-print-document="baseline"]');
    await expect(printed.locator("[data-print-cover]")).toContainText(`Baseline of Sections (${SECTION_KEY})`);
    await expect(printed.locator("[data-print-entry]")).toHaveCount(3);
    await expect(printed.locator(`[data-print-entry="${SECTION_KEY}-R2"]`)).toContainText("Brakes engage within 100 ms");
    await expect(printed.locator(`[data-print-entry="${SECTION_KEY}-R3"]`)).toContainText("Lights follow the pedal");
    await expect(printed.locator(`[data-print-entry="${SECTION_KEY}-R4"]`)).toHaveCount(0);
    await expect(printed).not.toContainText("within 80 ms");
    await expectAccessible(page);
  });

  test("alice downloads a baseline's matrix compared with now as a workbook", async ({ page, api }) => {
    const project = { params: { path: { projectKey: XLSX_KEY } } };
    must(await api.POST("/projects", { body: { key: XLSX_KEY, name: "Workbook" } }));
    for (const title of ["Brakes engage within 100 ms", "Brake pedal reads within 10 ms", "Brake bench run"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title } }));
    }
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${XLSX_KEY}-R2` } },
        body: { typeKey: "derives", otherKey: `${XLSX_KEY}-R1` },
      }),
    );
    const taken = must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Signed off" } })).baseline;
    must(
      await api.POST("/requirements/{reqKey}/links", {
        params: { path: { reqKey: `${XLSX_KEY}-R3` } },
        body: { typeKey: "verifies", otherKey: `${XLSX_KEY}-R1` },
      }),
    );

    await page.goto(`/p/${XLSX_KEY}/baselines/${taken.id}`);
    await page.getByRole("button", { name: "Matrix", exact: true }).click();
    await page.getByLabel("Compare the matrix with").selectOption("now");
    await expect(page.getByRole("table", { name: "Matrix" }).locator(`[data-cell="${XLSX_KEY}-R1:${XLSX_KEY}-R3"] [data-change="added"]`)).toBeVisible();
    const download = page.waitForEvent("download");
    await page.locator('[data-action="baseline-matrix-xlsx"]').click();
    const saved = await download;
    // Named as the CSV beside it is; the project's own matrix carries the key instead.
    expect(saved.suggestedFilename()).toBe("baseline-matrix.xlsx");
    const bytes = await readFile((await saved.path()) ?? "");
    expect(bytes.subarray(0, 2).toString()).toBe("PK");
    expect(Object.values(unzipText(bytes)).join("\n")).toContain("Verifies (added)");
  });

  test("alice changes who signs to bob and a group, both sign, and signing stops", async ({ page, api, pageAs, people }) => {
    const project = { params: { path: { projectKey: SIGN_KEY } } };
    must(await api.POST("/projects", { body: { key: SIGN_KEY, name: "Released twice" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    must(
      await api.PUT("/projects/{projectKey}/baseline-signing", {
        ...project,
        body: { required: 1, signers: [{ subjectType: "user", userId: people.alice.id }] },
      }),
    );
    await withDatabase(async (db) => {
      const made = await db.query<{ id: string }>("INSERT INTO groups (name) VALUES ($1) RETURNING id", [GROUP]);
      await db.query("INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)", [made.rows[0]!.id, people.alice.id]);
    });

    await page.goto(`/p/${SIGN_KEY}/settings?tab=approval`);
    const rule = page.locator("[data-signing-rule]");
    await expect(rule).toContainText(`1 signature of ${people.alice.name}`);
    await rule.locator('[data-action="edit-signing-rule"]').click();
    await rule.getByRole("button", { name: `Remove ${people.alice.name}` }).click();
    const picker = rule.getByRole("combobox", { name: "Add a person or group" });
    await picker.pressSequentially(people.bob.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await picker.pressSequentially(GROUP);
    await page.getByRole("option", { name: new RegExp(GROUP) }).click();
    await rule.getByLabel("Signatures needed").fill("2");
    await expectAccessible(page);
    await rule.getByRole("button", { name: "Save" }).click();
    await expect(rule.locator("[data-signing-rule-summary]")).toContainText("2 signatures of");
    await expect(rule.locator("[data-signing-rule-summary]")).toContainText(people.bob.name);
    await expect(rule.locator("[data-signing-rule-summary]")).toContainText(GROUP);
    await expect(rule.locator("[data-signing-rule-summary]")).not.toContainText(people.alice.name);
    const taken = must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 1.0" } })).baseline;
    const baselinePage = `/p/${SIGN_KEY}/baselines/${taken.id}`;

    const bob = await pageAs("bob");
    const bobs = bob.locator("[data-baseline-signing]");
    await openUntil(bob, baselinePage, () => expect(bobs.getByRole("button", { name: "Sign" })).toBeVisible(ONE_LOOK));
    await bobs.getByRole("button", { name: "Sign" }).click();
    const bobDialog = bob.getByRole("dialog", { name: "Sign the baseline" });
    await bobDialog.getByLabel("Your password").fill(people.bob.password);
    await bobDialog.getByRole("button", { name: "Sign and release" }).click();
    await expect(bobDialog).toHaveCount(0);
    await expect(bobs).toContainText("Signing: 1 of 2 signatures");

    const marked = page.locator('[data-baseline="Release 1.0"] [data-baseline-signed="signing"]');
    await openUntil(page, `/p/${SIGN_KEY}/baselines`, () => expect(marked).toBeVisible(ONE_LOOK));
    await expect(marked).toContainText("Signing: 1 of 2 signatures");

    const signing = page.locator("[data-baseline-signing]");
    await page.goto(baselinePage);
    await expect(signing).toContainText("Signing: 1 of 2 signatures");
    await signing.getByRole("button", { name: "Sign" }).click();
    const dialog = page.getByRole("dialog", { name: "Sign the baseline" });
    await dialog.getByLabel("Your password").fill(people.alice.password);
    await dialog.getByRole("button", { name: "Sign and release" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(signing).toContainText("Signed, with 2 of 2 signatures");
    await expect(signing.locator("[data-baseline-signatures] li")).toHaveCount(2);

    await page.goto(`/p/${SIGN_KEY}/settings?tab=approval`);
    await rule.locator('[data-action="remove-signing-rule"]').click();
    await confirm(page);
    await expect(rule).toContainText("Baselines are no longer signed.");
    await expect(rule.locator("[data-signing-rule-summary]")).toHaveText("Nobody signs this project's baselines.");

    await page.goto(baselinePage);
    await expect(signing).toContainText("Signed, with 2 of 2 signatures");
    const later = must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 2.0" } })).baseline;
    await openUntil(page, `/p/${SIGN_KEY}/baselines/${later.id}`, () => expect(page.locator('[data-baseline-signing="none"]')).toBeVisible(ONE_LOOK));
    await expect(page.getByRole("button", { name: "Sign" })).toHaveCount(0);
  });
});
