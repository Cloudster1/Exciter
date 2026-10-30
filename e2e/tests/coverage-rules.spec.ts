import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { uploadPassingResult } from "../fixtures/evidence";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { setProjectGrants } from "../fixtures/grants";

// Coverage rules for one type and the sources they count, and the report as its reader walks it (#314).

const KEY = uniqueKey("COVT");
// COVERAGE_PAGE_SIZE in web/src/config.ts: the report shows this many requirements at a time.
const COVERAGE_PAGE_SIZE = 50;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function write(api: ExciterApi, title: string, typeId?: string) {
  return must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title, typeId } })).requirement;
}

function rowOf(page: Page, reqKey: string) {
  return page.getByRole("list", { name: "Requirements" }).locator(`[data-coverage-row="${reqKey}"]`);
}

test.describe("coverage rules", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice gives one type a rule of its own, asks it to be verified, and removes the rule again", async ({ page, api }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Typed coverage" } }));
    const note = must(await api.POST("/projects/{projectKey}/types", { ...project, body: { name: "Note", icon: "test", colour: "#22aa55" } })).type;
    await write(api, "Brakes engage within 100 ms");
    await write(api, "Brakes were discussed at length", note.id);
    must(await api.PUT("/projects/{projectKey}/coverage-rules", { ...project, body: { decomposed: true, implemented: false, verified: false } }));

    await page.goto(`/p/${KEY}/coverage`);
    const rules = page.getByRole("list", { name: "Rules" });
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Decomposed: None");
    await page.getByLabel("Add a rule for").selectOption({ label: "Note" });
    await page.getByRole("button", { name: "Add", exact: true }).click();
    const noteRule = rules.locator('[data-rule="Note"]');
    await expect(noteRule).toBeVisible();
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Decomposed: Not asked");

    const verified = noteRule.getByLabel("Verified", { exact: true });
    await verified.click();
    await expect(verified).toBeChecked();
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Verified: None");
    await expect(rowOf(page, `${KEY}-R1`)).toContainText("Verified: Not asked");
    await expect(rowOf(page, `${KEY}-R1`)).toContainText("Decomposed: None");
    await expectAccessible(page);

    await noteRule.getByRole("button", { name: "Remove the rule for Note" }).click();
    await expect(noteRule).toHaveCount(0);
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Verified: Not asked");
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Decomposed: None");
  });

  test("alice counts passing test results instead of links as what verifies", async ({ page, api }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Sourced coverage" } }));
    await write(api, "Brakes engage within 100 ms");
    await write(api, "Brake lights follow the pedal");
    await write(api, "Brake light bench run");
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-R3` } }, body: { typeKey: "verifies", otherKey: `${KEY}-R2` } }));
    must(await api.PUT("/projects/{projectKey}/coverage-rules", { ...project, body: { decomposed: false, implemented: false, verified: true } }));
    await uploadPassingResult(api, KEY, `${KEY}-R1`);

    await page.goto(`/p/${KEY}/coverage`);
    await expect(rowOf(page, `${KEY}-R1`)).toContainText("Verified: None");
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Verified: Full");
    const sources = page.getByRole("group", { name: "Verified from: Every other type" });
    const results = sources.getByRole("button", { name: "Test results" });
    await results.click();
    await expect(results).toHaveAttribute("aria-pressed", "true");
    const links = sources.getByRole("button", { name: "Links" });
    await links.click();
    await expect(links).toHaveAttribute("aria-pressed", "false");

    await expect(rowOf(page, `${KEY}-R1`)).toContainText("Verified: Full");
    await expect(rowOf(page, `${KEY}-R2`)).toContainText("Verified: None");
    await expectAccessible(page);
    const saved = must(await api.GET("/projects/{projectKey}/coverage-rules", project)).rules;
    expect(saved.find((r) => r.typeId === null)?.verifiedFrom).toEqual(["tests"]);
  });

  test("bob, who reads the project, sees the rules he may not change and walks the requirements a page at a time", async ({ api, pageAs, people }) => {
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Read coverage" } }));
    const me = must(await api.GET("/auth/me")).user;
    await setProjectGrants(api, KEY, [
      { subject: { type: "user", id: me.id }, role: "admin" },
      { subject: { type: "user", id: people.bob.id }, role: "viewer" },
    ]);
    must(await api.PUT("/projects/{projectKey}/coverage-rules", { ...project, body: { decomposed: true, implemented: false, verified: false } }));
    const total = COVERAGE_PAGE_SIZE + 1;
    for (let n = 1; n <= total; n++) await write(api, `Brake requirement ${n}`);
    const last = `${KEY}-R${total}`;

    const bob = await pageAs("bob");
    await bob.goto(`/p/${KEY}/coverage`);
    await expect(bob.getByText("Only the project's administrators set what coverage asks.")).toBeVisible();
    const every = bob.getByRole("list", { name: "Rules" }).locator('[data-rule="Every other type"]');
    for (const box of ["Decomposed", "Implemented", "Verified"]) await expect(every.getByLabel(box, { exact: true })).toBeDisabled();
    await expect(bob.getByLabel("Add a rule for")).toHaveCount(0);

    const pages = bob.getByRole("navigation", { name: "Requirements" });
    await expect(pages).toContainText(`1 to ${COVERAGE_PAGE_SIZE} of ${total}`);
    await expect(pages.getByRole("button", { name: "Previous" })).toBeDisabled();
    await expect(rowOf(bob, last)).toHaveCount(0);
    await expectAccessible(bob);
    await pages.getByRole("button", { name: "Next" }).click();
    await expect(pages).toContainText(`${total} to ${total} of ${total}`);
    await expect(pages.getByRole("button", { name: "Next" })).toBeDisabled();
    await expect(rowOf(bob, last)).toContainText("Decomposed: None");
    await pages.getByRole("button", { name: "Previous" }).click();
    await expect(pages).toContainText(`1 to ${COVERAGE_PAGE_SIZE} of ${total}`);
    await expect(rowOf(bob, `${KEY}-R1`)).toBeVisible();

    await pages.getByRole("button", { name: "Next" }).click();
    await rowOf(bob, last).getByRole("link").click();
    await expect(bob).toHaveURL(new RegExp(`/p/${KEY}/r/${last}$`));
    await expect(bob.getByRole("heading", { level: 1 })).toHaveText(`Brake requirement ${total}`);
  });
});
