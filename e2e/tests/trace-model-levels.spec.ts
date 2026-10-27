import { readFile } from "node:fs/promises";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The trace model's levels moved, removed and ending in tests or code, and the chains narrowed by level, downloaded
// and shared as a view (#315).

const MODEL_KEY = uniqueKey("TMLV");
const CHAINS_KEY = uniqueKey("TMCH");

async function removeProject(api: ExciterApi, projectKey: string) {
  const path = { params: { path: { projectKey } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function makeTypes(api: ExciterApi, projectKey: string, names: string[]): Promise<Record<string, string>> {
  const types: Record<string, string> = {};
  for (const name of names) {
    types[name] = must(
      await api.POST("/projects/{projectKey}/types", { params: { path: { projectKey } }, body: { name, icon: "test", colour: "#2255aa" } }),
    ).type.id;
  }
  return types;
}

test.describe("trace model levels and chains", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api, MODEL_KEY);
    await removeProject(api, CHAINS_KEY);
  });

  test("alice moves and removes levels, ends the model in tests and then code, and reads a refusal by its level", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: MODEL_KEY, name: "Levels" } }));
    await makeTypes(api, MODEL_KEY, ["Need", "System"]);

    await page.goto(`/p/${MODEL_KEY}/settings?tab=trace`);
    const names = page.getByLabel("Name");
    const holds = page.getByLabel("Holds");
    for (const [i, name] of ["Need", "System", "Check"].entries()) {
      await page.getByRole("button", { name: "Add a level" }).click();
      await names.nth(i).fill(name);
    }
    await page.getByRole("group", { name: "Types: Need" }).getByRole("button", { name: "Need" }).click();
    await page.getByRole("group", { name: "Types: System" }).getByRole("button", { name: "System" }).click();
    await page.getByRole("group", { name: "Reached from the level before by: System" }).getByRole("button", { name: "Is derived into" }).click();
    await holds.nth(2).selectOption({ label: "Test results" });
    await page.getByRole("group", { name: "Reached from the level before by: Check" }).getByRole("button", { name: "Verified by" }).click();

    await page.getByRole("button", { name: "Move Check up" }).click();
    await expect(names.nth(1)).toHaveValue("Check");
    await expect(page.getByRole("button", { name: "Move Need up" })).toBeDisabled();
    await page.getByRole("button", { name: "Save the model" }).click();
    await expect(page.locator('[data-trace-level="1"]')).toContainText("Issues, pages, tests and code end a model; move this level to the end.");
    await expect(page.getByText("The trace model is saved.")).toHaveCount(0);
    await expectAccessible(page);

    await page.getByRole("button", { name: "Move Check down" }).click();
    await expect(page.getByRole("button", { name: "Move Check down" })).toBeDisabled();
    await page.getByRole("button", { name: "Remove System" }).click();
    await expect(names).toHaveCount(2);
    await page.getByRole("button", { name: "Save the model" }).click();
    await expect(page.getByText("The trace model is saved.")).toBeVisible();
    await page.reload();
    await expect(names.nth(1)).toHaveValue("Check");
    await expect(holds.nth(1)).toHaveValue("tests");
    await expect(page.getByRole("group", { name: "Reached from the level before by: Check" }).getByRole("button", { name: "Verified by" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    await holds.nth(1).selectOption({ label: "Code" });
    await page.getByRole("group", { name: "Reached from the level before by: Check" }).getByRole("button", { name: "Implemented by" }).click();
    await page.getByRole("button", { name: "Save the model" }).click();
    await expect(page.getByText("The trace model is saved.")).toBeVisible();
    await page.reload();
    await expect(holds.nth(1)).toHaveValue("code");
    await expect(names).toHaveCount(2);
  });

  test("alice narrows the chains by level, downloads them, and bob opens her shared view so", async ({ page, api, pageAs }) => {
    const projectKey = { params: { path: { projectKey: CHAINS_KEY } } };
    must(await api.POST("/projects", { body: { key: CHAINS_KEY, name: "Chains by level" } }));
    const types = await makeTypes(api, CHAINS_KEY, ["Need", "System", "Test"]);
    for (const [title, type] of [
      ["Stop within 40 m", "Need"],
      ["Brakes engage within 100 ms", "System"],
      ["Brake bench run", "Test"],
    ] as const) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...projectKey, body: { title, typeId: types[type] } }));
    }
    for (const [from, typeKey, to] of [
      ["R2", "derives", "R1"],
      ["R3", "verifies", "R2"],
    ] as const) {
      must(
        await api.POST("/requirements/{reqKey}/links", {
          params: { path: { reqKey: `${CHAINS_KEY}-${from}` } },
          body: { typeKey, otherKey: `${CHAINS_KEY}-${to}` },
        }),
      );
    }
    const linkTypes = must(await api.GET("/projects/{projectKey}/link-types", projectKey)).linkTypes;
    const typeId = (key: string) => linkTypes.find((each) => each.key === key)!.id;
    must(
      await api.PUT("/projects/{projectKey}/trace-model", {
        ...projectKey,
        body: {
          levels: [
            { name: "Need", kind: "requirements", typeIds: [types.Need!] },
            { name: "System", kind: "requirements", typeIds: [types.System!], steps: [{ typeId: typeId("derives"), direction: "incoming" }] },
            { name: "Test", kind: "requirements", typeIds: [types.Test!], steps: [{ typeId: typeId("verifies"), direction: "incoming" }] },
          ],
        },
      }),
    );

    await page.goto(`/p/${CHAINS_KEY}/chains`);
    const table = page.getByRole("table", { name: "Chains" });
    await expect(table.getByRole("columnheader")).toHaveText(["Need", "System", "Test"]);
    await page.getByLabel("To level").selectOption({ label: "System" });
    await expect(table.getByRole("columnheader")).toHaveText(["Need", "System"]);
    await page.getByLabel("To level").selectOption({ label: "Test" });
    await page.getByLabel("From level").selectOption({ label: "System" });
    await expect(table.getByRole("columnheader")).toHaveText(["System", "Test"]);
    await expect(table.locator("[data-chain-line]").first()).toContainText(`${CHAINS_KEY}-R2`);
    await expectAccessible(page);

    await page.reload();
    await expect(page.getByLabel("From level")).toHaveValue("1");
    await expect(page.getByLabel("To level")).toHaveValue("2");
    await expect(table.getByRole("columnheader")).toHaveText(["System", "Test"]);

    const download = page.waitForEvent("download");
    await page.locator('[data-action="chains-csv"]').click();
    const [header, line] = (await readFile((await (await download).path()) ?? "", "utf8")).split(/\r?\n/);
    // The file opens with a byte order mark, so its heading is read from the end.
    expect(header?.endsWith("System,Test,Breaks after") && !header.includes("Need")).toBe(true);
    expect(line).toContain(`${CHAINS_KEY}-R2`);
    expect(line).toContain(`${CHAINS_KEY}-R3`);
    const workbook = page.waitForEvent("download");
    await page.locator('[data-action="chains-xlsx"]').click();
    const saved = await workbook;
    expect(saved.suggestedFilename()).toBe(`${CHAINS_KEY}-chains.xlsx`);
    expect((await readFile((await saved.path()) ?? "")).subarray(0, 2).toString()).toBe("PK");

    await page.getByRole("button", { name: "Save as a view" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name").fill("Systems down");
    await dialog.getByLabel("Share with everybody in the project").check();
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Saved the view Systems down." })).toBeVisible();

    const bob = await pageAs("bob");
    await bob.goto(`/p/${CHAINS_KEY}/chains`);
    await bob.getByLabel("Views").selectOption({ label: "Systems down (Shared)" });
    await expect(bob.getByLabel("From level")).toHaveValue("1");
    await expect(bob.getByRole("table", { name: "Chains" }).getByRole("columnheader")).toHaveText(["System", "Test"]);
    await expectAccessible(bob);
  });
});
