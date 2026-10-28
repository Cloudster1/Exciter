import { readFile } from "node:fs/promises";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { unzipText } from "../fixtures/unzip";

// Every control of the Risks settings tab, the FMEA table, the risk matrix and the three downloads, each checked for
// what it changes (#325).

const SIZE = 5;
// One more risk than a page of the FMEA table holds, the server's risk.DefaultRows.
const PAGE = 50;
const KEYS = {
  method: uniqueKey("RCM"),
  levels: uniqueKey("RCL"),
  table: uniqueKey("RCT"),
  matrix: uniqueKey("RCX"),
  files: uniqueKey("RCF"),
};

type Side = { severity: number | null; probability: number | null; detection: null };
const side = (severity: number | null, probability: number | null): Side => ({ severity, probability, detection: null });

async function removeProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** Five values a scale, high from 12 and medium from 5 by the product of severity and probability. */
function scheme() {
  const values = (name: string) => Array.from({ length: SIZE }, (_, i) => ({ value: i + 1, name: `${name} ${i + 1}`, description: "" }));
  const matrix = [];
  for (let severity = 1; severity <= SIZE; severity++) {
    for (let probability = 1; probability <= SIZE; probability++) {
      const product = severity * probability;
      matrix.push({ severity, probability, level: product >= 12 ? "High" : product >= 5 ? "Medium" : "Low" });
    }
  }
  return {
    method: "matrix" as const,
    detection: false,
    scales: [
      { dimension: "severity" as const, label: "Severity", values: values("Severity") },
      { dimension: "probability" as const, label: "Probability", values: values("Probability") },
    ],
    levels: [
      { name: "Low", colour: "#0f9d8a", acceptable: true, minRpn: null },
      { name: "Medium", colour: "#d97706", acceptable: false, minRpn: null },
      { name: "High", colour: "#c2410c", acceptable: false, minRpn: null },
    ],
    matrix,
  };
}

/** A project with a type of risks and the five by five scheme, answering the type and the project. */
async function riskProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const project = must(await api.POST("/projects", { body: { key, name: "Brakes" } })).project;
  const hazard = must(
    await api.POST("/projects/{projectKey}/types", { ...path, body: { name: "Hazard", icon: "risk", colour: "#c2410c", role: "risk" } }),
  ).type;
  must(await api.PUT("/projects/{projectKey}/risk-scheme", { ...path, body: scheme() }));
  const write = async (title: string, before?: Side, after?: Side) =>
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: { title, typeId: hazard.id, ...(before ? { risk: { before, after: after ?? side(null, null) } } : {}) },
      }),
    ).requirement.key;
  return { project, hazard, path, write };
}

/** The file a link of the page downloads, saved where Playwright put it. */
async function downloaded(page: Page, action: string): Promise<Buffer> {
  const download = page.waitForEvent("download");
  await page.locator(`[data-action="${action}"]`).click();
  return readFile((await (await download).path()) ?? "");
}

async function runQuery(page: Page, query: string) {
  await page.getByRole("combobox", { name: "Query" }).fill(query);
  await page.locator('[data-action="run-query"]').click();
}

test.describe("risk controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    for (const key of Object.values(KEYS)) await removeProject(api, key);
  });

  test("alice judges by priority number with detection, and a rated hazard takes the level it gives", async ({ page, api }) => {
    // It saves the scheme, then rates detection in the table, each page checked for accessibility.
    test.setTimeout(60_000);
    const key = KEYS.method;
    const { write } = await riskProject(api, key);
    const risk = await write("Brakes fail on ice", side(4, 3));

    await page.goto(`/p/${key}/risks`);
    const row = page.locator(`[data-fmea-row="${risk}"]`);
    await expect(row.locator('[data-risk-level-tag="High"]').first()).toBeVisible();

    await page.goto(`/p/${key}/settings?tab=risks`);
    await page.getByLabel("A rating gives its level by").selectOption("rpn");
    await expect(page.getByRole("table", { name: "Matrix" })).toHaveCount(0);
    await page.getByRole("checkbox", { name: "Rate detection too" }).check();
    const detection = page.getByRole("group", { name: "Detection" });
    await detection.getByLabel("Detection 1", { exact: true }).fill("Found at once");
    await detection.getByLabel("Detection 2", { exact: true }).fill("Found in review");
    await page.getByLabel("Medium from priority number").fill("10");
    await page.getByLabel("High from priority number").fill("30");
    await expectAccessible(page);
    await page.locator('[data-action="save-risk-scheme"]').click();
    await expect(page.getByText("The risk scheme is saved.")).toBeVisible();
    const saved = must(await api.GET("/projects/{projectKey}/risk-scheme", { params: { path: { projectKey: key } } })).scheme!;
    expect(saved.method).toBe("rpn");
    expect(saved.levels.map((l) => l.minRpn)).toEqual([null, 10, 30]);
    expect(saved.scales.find((s) => s.dimension === "detection")?.values[0]?.name).toBe("Found at once");

    await page.goto(`/p/${key}/risks`);
    await expect(row.locator("[data-risk-level-tag]")).toHaveCount(0);
    await row.getByRole("combobox", { name: `Detection before mitigation of ${risk}` }).selectOption("2");
    // 4 x 3 x 2 is 24: Medium from 10, High only from 30.
    await expect(row.locator('[data-risk-level-tag="Medium"]')).toHaveCount(2);
    await expect(row.locator('[data-risk-level-tag="High"]')).toHaveCount(0);
    await expectAccessible(page);
  });

  test("alice adds a level with its cells, removes it again, and grows a scale while a rating holds its top", async ({ page, api }) => {
    // It saves the scheme twice and reads the table between, each page checked for accessibility.
    test.setTimeout(60_000);
    const key = KEYS.levels;
    const { write, path } = await riskProject(api, key);
    const risk = await write("Brakes fail on ice", side(5, 5));

    await page.goto(`/p/${key}/settings?tab=risks`);
    await page.locator('[data-action="add-risk-level"]').click();
    await page.getByLabel("Name of level 4").fill("Critical");
    await page.getByLabel("Pick Colour of level 4").fill("#7c3aed");
    await expect(page.getByRole("textbox", { name: "Colour of level 4", exact: true })).toHaveValue("#7c3aed");
    await page.getByRole("checkbox", { name: "Critical is acceptable" }).check();
    const matrix = page.getByRole("table", { name: "Matrix" });
    const corner = matrix.getByRole("combobox", { name: "Level at severity 5 and probability 5" });
    await corner.selectOption("Critical");
    await expectAccessible(page);
    await page.locator('[data-action="save-risk-scheme"]').click();
    await expect(page.getByText("The risk scheme is saved.")).toBeVisible();
    const added = must(await api.GET("/projects/{projectKey}/risk-scheme", path)).scheme!;
    expect(added.levels[3]).toEqual({ name: "Critical", colour: "#7c3aed", acceptable: true, minRpn: null });

    await page.goto(`/p/${key}/risks`);
    await expect(page.locator(`[data-fmea-row="${risk}"] [data-risk-level-tag="Critical"]`).first()).toBeVisible();

    await page.goto(`/p/${key}/settings?tab=risks`);
    await page.getByRole("button", { name: "Remove the level Critical" }).click();
    await expect(corner).toHaveValue("Low");
    await expect(corner.locator("option", { hasText: "Critical" })).toHaveCount(0);
    await page.locator('[data-action="save-risk-scheme"]').click();
    await expect(page.getByText("The risk scheme is saved.")).toBeVisible();
    const removed = must(await api.GET("/projects/{projectKey}/risk-scheme", path)).scheme!;
    expect(removed.levels.map((l) => l.name)).toEqual(["Low", "Medium", "High"]);
    expect(removed.matrix.find((c) => c.severity === 5 && c.probability === 5)?.level).toBe("Low");

    const severity = page.getByRole("group", { name: "Severity" });
    const shrink = severity.getByRole("button", { name: "Remove the highest value of severity" });
    // The hazard is rated severity 5, so the scale keeps it.
    await expect(shrink).toBeDisabled();
    await severity.getByRole("button", { name: "Add a value to severity" }).click();
    await expect(matrix.getByRole("combobox", { name: "Level at severity 6 and probability 1" })).toBeVisible();
    await expect(severity.getByLabel("Severity 6", { exact: true })).toHaveValue("Severity 6");
    await expect(shrink).toBeEnabled();
    await shrink.click();
    await expect(matrix.getByRole("combobox", { name: "Level at severity 6 and probability 1" })).toHaveCount(0);
    await expect(shrink).toBeDisabled();
    await expectAccessible(page);
  });

  test("alice rates a risk in the table, turns its pages and narrows it with a query the export carries", async ({ page, api }) => {
    // It writes a page and one more of risks, then reads the table, a risk's history and the table again.
    test.setTimeout(120_000);
    const key = KEYS.table;
    const { write } = await riskProject(api, key);
    const rated = await write("Brakes fail on ice", side(4, 3));
    let last = rated;
    for (let i = 1; i <= PAGE; i++) last = await write(`Hazard ${i}`);

    await page.goto(`/p/${key}/risks`);
    const row = page.locator(`[data-fmea-row="${rated}"]`);
    await expect(row.locator('[data-risk-level-tag="High"]')).toHaveCount(2);
    await row.getByRole("combobox", { name: `Severity before mitigation of ${rated}` }).selectOption("1");
    // 1 x 3 is 3, which the matrix gives Low.
    await expect(row.locator('[data-risk-level-tag="Low"]')).toHaveCount(2);
    await expect(row.locator('[data-risk-level-tag="High"]')).toHaveCount(0);
    await expectAccessible(page);

    const next = page.getByRole("button", { name: "Next" });
    const previous = page.getByRole("button", { name: "Previous" });
    await expect(previous).toBeDisabled();
    await expect(page.getByText(`1 to ${PAGE} of ${PAGE + 1}`)).toBeVisible();
    await next.click();
    await expect(page.getByText(`${PAGE + 1} to ${PAGE + 1} of ${PAGE + 1}`)).toBeVisible();
    await expect(page.locator(`[data-fmea-row="${last}"]`)).toBeVisible();
    await expect(row).toHaveCount(0);
    await expect(next).toBeDisabled();
    await previous.click();
    await expect(row).toBeVisible();

    await runQuery(page, "rpn >= 3");
    await expect(page.locator("[data-fmea-row]")).toHaveCount(1);
    await expect(row).toBeVisible();
    await expect(next).toHaveCount(0);
    const href = (await page.locator('[data-action="export-risks"]').getAttribute("href")) ?? "";
    expect(new URL(href, "http://exciter.test").searchParams.get("query")).toBe("rpn >= 3");
    await expectAccessible(page);

    await page.goto(`/p/${key}/r/${rated}`);
    await page.locator("[data-history-link]").click();
    await expect(page.locator('[data-revision-row="2"]')).toBeVisible();
  });

  test("the matrix moves a mitigated hazard between its cells as the side changes, and a query narrows its counts", async ({ page, api }) => {
    // It reads the matrix on both sides and narrowed, each checked for accessibility.
    test.setTimeout(60_000);
    const key = KEYS.matrix;
    const { project, path, write } = await riskProject(api, key);
    must(
      await api.PUT("/projects/{projectKey}/coverage-rules", {
        ...path,
        body: { typeId: project.settings.defaultTypeId ?? undefined, decomposed: false, implemented: false, verified: true },
      }),
    );
    const control = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop on ice" } })).requirement.key;
    const hazard = await write("Brakes fail on ice", side(4, 3), side(2, 3));
    const check = must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Check the brakes" } })).requirement.key;
    await write("Brakes squeal", side(1, 1));
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: control } }, body: { typeKey: "mitigates", otherKey: hazard } }));
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: check } }, body: { typeKey: "verifies", otherKey: control } }));

    await page.goto(`/p/${key}/risks?view=matrix`);
    const cell = (at: string) => page.locator(`[data-risk-cell="${at}"]`);
    await expect(cell("2:3")).toHaveAttribute("data-count", "1");
    await expect(cell("4:3")).toHaveAttribute("data-count", "0");
    await page.locator('[data-side="before"]').click();
    await expect(cell("4:3")).toHaveAttribute("data-count", "1");
    await expect(cell("2:3")).toHaveAttribute("data-count", "0");
    await expectAccessible(page);
    await page.locator('[data-side="effective"]').click();
    await expect(cell("2:3")).toHaveAttribute("data-count", "1");
    await expect(cell("4:3")).toHaveAttribute("data-count", "0");

    await expect(cell("1:1")).toHaveAttribute("data-count", "1");
    await runQuery(page, "title ~ ice");
    await expect(cell("1:1")).toHaveAttribute("data-count", "0");
    await expect(cell("2:3")).toHaveAttribute("data-count", "1");
    await expectAccessible(page);
  });

  test("the downloads hold the risks, and a query narrows the printed report", async ({ page, api }) => {
    // The PDF is printed by the render service, which takes a while.
    test.setTimeout(120_000);
    const key = KEYS.files;
    const { write } = await riskProject(api, key);
    const ice = await write("Brakes fail on ice", side(4, 3));
    const squeal = await write("Brakes squeal", side(1, 1));

    await page.goto(`/p/${key}/risks`);
    await expect(page.locator(`[data-fmea-row="${ice}"]`)).toBeVisible();
    const workbook = unzipText(await downloaded(page, "export-risks"));
    expect(workbook["xl/sharedStrings.xml"]).toContain("Brakes fail on ice");
    expect(workbook["xl/sharedStrings.xml"]).toContain("Brakes squeal");
    const word = unzipText(await downloaded(page, "risk-report-docx"));
    expect(word["word/document.xml"]).toContain(`${ice} Brakes fail on ice`);
    const pdf = await downloaded(page, "risk-report-pdf");
    expect(pdf.subarray(0, 5).toString("latin1")).toBe("%PDF-");

    await page.goto(`/print/risks/${key}?query=${encodeURIComponent("title ~ ice")}`);
    await expect(page.getByRole("heading", { name: `${ice} Brakes fail on ice` })).toBeVisible();
    await expect(page.getByRole("heading", { name: `${squeal} Brakes squeal` })).toHaveCount(0);
    await expectAccessible(page);
  });
});
