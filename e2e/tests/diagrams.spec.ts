import type { Page } from "@playwright/test";
import { uniqueKey } from "../fixtures/seed";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("DGM");

/** Removes the project the test made, so each test starts without it. */
async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
}

/** Whether a picture on the page was drawn, not left broken. */
async function drawn(page: Page, selector: string) {
  return page.locator(selector).first().evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0);
}

// Diagrams (#284): written as PlantUML in the editor with a preview Kroki draws, and shown drawn wherever the
// requirement is read.
test.describe("diagrams", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice draws a PlantUML diagram in a statement and reads it drawn on the requirement and in the document", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Diagrams" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Pump control" } }));
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await page.getByRole("button", { name: "Edit" }).click();

    const frame = page.locator("[data-editor-frame]").filter({ has: page.locator("#edit-statement") });
    await frame.getByRole("button", { name: "PlantUML diagram" }).click();
    const source = frame.getByRole("textbox", { name: "PlantUML source" });
    await expect(source).toBeVisible();
    await expect(frame.getByRole("img", { name: "Components" })).toBeVisible();

    await source.fill("@startuml\ntitle Pump and valve\n[Pump] --> [Valve] : opens\n@enduml");
    const preview = frame.getByRole("img", { name: "Pump and valve" });
    await expect(preview).toBeVisible();
    await expect.poll(() => drawn(page, "[data-diagram-preview] img")).toBe(true);
    await expectAccessible(page);

    await source.fill("@startuml\nclass A {\nfoo(\n@enduml");
    await expect(frame.locator("[data-diagram-error]")).toContainText("The diagram cannot be drawn");
    await source.fill("@startuml\ntitle Pump and valve\n[Pump] --> [Valve] : opens\n@enduml");
    await expect(preview).toBeVisible();

    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeAttached();
    const shown = page.locator("[data-statement]").getByRole("img", { name: "Pump and valve" });
    await expect(shown).toBeVisible();
    await expect.poll(() => drawn(page, "[data-statement] [data-diagram] img")).toBe(true);
    const download = page.waitForEvent("download");
    await page.locator("[data-statement]").getByRole("button", { name: "Save as SVG" }).click();
    expect((await download).suggestedFilename()).toBe("diagram.svg");
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/spec`);
    const entry = page.locator(`[data-spec-entry="${KEY}-R1"]`);
    await expect(entry.getByRole("img", { name: "Pump and valve" })).toBeVisible();
    await expect.poll(() => drawn(page, `[data-spec-entry="${KEY}-R1"] [data-diagram] img`)).toBe(true);

    // The print view is what the PDF and the static site are made of: the drawing is in it once it says it is ready.
    await page.goto(`/print/p/${KEY}/document`);
    await expect(page.locator("html[data-print-ready]")).toBeAttached();
    await expect(page.getByRole("img", { name: "Pump and valve" })).toBeVisible();
    expect(await page.getByRole("img", { name: "Pump and valve" }).getAttribute("src")).toMatch(/^data:image\/svg\+xml/);

    const md = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` }, query: { format: "markdown" } } }));
    expect(md.requirement.statementMarkdown).toBe("```plantuml\n@startuml\ntitle Pump and valve\n[Pump] --> [Valve] : opens\n@enduml\n```");
  });
});
