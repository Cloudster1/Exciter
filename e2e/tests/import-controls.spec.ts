import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { wordDocument, wordTemplate } from "../fixtures/docx";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const CSV = uniqueKey("ICV");
const REQIF = uniqueKey("IRQ");
const SOURCE = uniqueKey("IRS");
const WORD = uniqueKey("IWD");
const OWNER = uniqueKey("ITO");
const OTHER = uniqueKey("ITH");

// A file this long is still being imported when the test stops it, however quick the worker.
const LONG_IMPORT = 2000;
// How long a test waits for the stack's worker to end an import.
const IMPORT_WAIT_MS = 30_000;

async function cleanUp(api: ExciterApi, keys: string[]) {
  for (const key of keys) {
    const path = { params: { path: { projectKey: key } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
  }
}

/** A CSV file of the given number of rows, each a requirement with a title of its own. */
function titledRows(n: number): Buffer {
  return Buffer.from(`title\n${Array.from({ length: n }, (_, i) => `Requirement ${i + 1}`).join("\n")}\n`);
}

/** A ReqIF document of the given number of requirements, one after the other in one specification. */
function manyObjects(n: number): Buffer {
  const changed = 'LAST-CHANGE="2026-09-02T10:00:00Z"';
  const ids = Array.from({ length: n }, (_, i) => i + 1);
  const objects = ids
    .map(
      (i) =>
        `<SPEC-OBJECT IDENTIFIER="_o${i}" ${changed}><TYPE><SPEC-OBJECT-TYPE-REF>_t_req</SPEC-OBJECT-TYPE-REF></TYPE><VALUES>` +
        `<ATTRIBUTE-VALUE-STRING THE-VALUE="Requirement ${i}"><DEFINITION><ATTRIBUTE-DEFINITION-STRING-REF>_a_name</ATTRIBUTE-DEFINITION-STRING-REF></DEFINITION>` +
        "</ATTRIBUTE-VALUE-STRING></VALUES></SPEC-OBJECT>",
    )
    .join("");
  const children = ids
    .map((i) => `<SPEC-HIERARCHY IDENTIFIER="_h${i}" ${changed}><OBJECT><SPEC-OBJECT-REF>_o${i}</SPEC-OBJECT-REF></OBJECT></SPEC-HIERARCHY>`)
    .join("");
  return Buffer.from(
    `<?xml version="1.0" encoding="UTF-8"?><REQ-IF xmlns="http://www.omg.org/spec/ReqIF/20110401/reqif.xsd">` +
      `<THE-HEADER><REQ-IF-HEADER IDENTIFIER="_hdr"><TITLE>Many</TITLE></REQ-IF-HEADER></THE-HEADER><CORE-CONTENT><REQ-IF-CONTENT>` +
      `<DATATYPES><DATATYPE-DEFINITION-STRING IDENTIFIER="_dt_string" ${changed} LONG-NAME="String" MAX-LENGTH="1000"/></DATATYPES>` +
      `<SPEC-TYPES><SPEC-OBJECT-TYPE IDENTIFIER="_t_req" ${changed} LONG-NAME="Requirement"><SPEC-ATTRIBUTES>` +
      `<ATTRIBUTE-DEFINITION-STRING IDENTIFIER="_a_name" ${changed} LONG-NAME="ReqIF.Name"><TYPE><DATATYPE-DEFINITION-STRING-REF>_dt_string</DATATYPE-DEFINITION-STRING-REF></TYPE></ATTRIBUTE-DEFINITION-STRING>` +
      `</SPEC-ATTRIBUTES></SPEC-OBJECT-TYPE><SPECIFICATION-TYPE IDENTIFIER="_st" ${changed} LONG-NAME="Module"/></SPEC-TYPES>` +
      `<SPEC-OBJECTS>${objects}</SPEC-OBJECTS><SPECIFICATIONS><SPECIFICATION IDENTIFIER="_spec" ${changed} LONG-NAME="Many">` +
      `<TYPE><SPECIFICATION-TYPE-REF>_st</SPECIFICATION-TYPE-REF></TYPE><CHILDREN>${children}</CHILDREN></SPECIFICATION></SPECIFICATIONS>` +
      "</REQ-IF-CONTENT></CORE-CONTENT></REQ-IF>",
  );
}

/** Uploads a file on the tab the page shows, and answers the id of the import the server made. */
async function upload(page: Page, label: string, action: string, file: { name: string; mimeType: string; buffer: Buffer }): Promise<string> {
  await page.getByLabel(label, { exact: true }).setInputFiles(file);
  const made = page.waitForResponse((res) => res.request().method() === "POST" && res.status() === 201);
  await page.locator(`[data-action="${action}"]`).click();
  return (await (await made).json()).import.id as string;
}

async function requirementsOf(api: ExciterApi, key: string): Promise<number> {
  return must(await api.GET("/projects/{projectKey}/tree", { params: { path: { projectKey: key } } })).nodes.length;
}

// The projects' keys are this worker's own, so they meet no other spec's.
test.describe("a CSV import past the happy path", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api, [CSV]));

  test("alice answers what matches nothing, then discards a file and stops a long one", async ({ page, api }) => {
    test.setTimeout(90_000);
    must(await api.POST("/projects", { body: { key: CSV, name: "Spreadsheet" } }));
    await page.goto(`/p/${CSV}/settings?tab=import`);
    const csv = (name: string, buffer: Buffer) => ({ name, mimeType: "text/csv", buffer });

    await upload(page, "CSV file", "upload-import", csv("problems.csv", Buffer.from("title,type,owner\nShip it,Feature,nobody@exciter.test\n")));
    const problems = page.locator("[data-import-problems]");
    await expect(problems).toHaveAttribute("data-import-problems", "2");
    await expect(problems).toContainText("Feature");
    await expect(problems).toContainText("nobody@exciter.test");
    await expect(page.locator('[data-action="start-import"]')).toBeDisabled();
    await expectAccessible(page);
    await page.getByRole("combobox", { name: "What Feature means here" }).selectOption({ label: "Requirement" });
    await expect(problems).toHaveAttribute("data-import-problems", "1");
    await page.getByRole("combobox", { name: "Who nobody@exciter.test is" }).selectOption({ label: "Nobody" });
    await expect(page.getByText("Nothing keeps the import from starting.")).toBeVisible();
    await page.locator('[data-action="start-import"]').click();
    await expect(page.locator('[data-import-step="done"]')).toContainText("1 requirement made", { timeout: IMPORT_WAIT_MS });
    const shipped = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${CSV}-R1` } } })).requirement;
    expect(shipped.title).toBe("Ship it");
    expect(shipped.owner).toBeFalsy();

    await page.locator('[data-action="import-another"]').click();
    const discarded = await upload(page, "CSV file", "upload-import", csv("again.csv", Buffer.from("title\nDiscard me\n")));
    await page.locator('[data-action="discard-import"]').click();
    await expect(page.locator("[data-import-upload]")).toBeVisible();
    expect((await api.GET("/imports/{importID}", { params: { path: { importID: discarded } } })).response.status).toBe(404);
    expect(await requirementsOf(api, CSV)).toBe(1);

    const long = await upload(page, "CSV file", "upload-import", csv("long.csv", titledRows(LONG_IMPORT)));
    await page.locator('[data-action="start-import"]').click();
    await page.locator('[data-action="stop-import"]').click();
    const stopped = page.locator('[data-import-step="cancelled"]');
    await expect(stopped).toContainText("Stopped", { timeout: IMPORT_WAIT_MS });
    const found = must(await api.GET("/imports/{importID}", { params: { path: { importID: long } } })).import;
    expect(found.state).toBe("cancelled");
    expect(found.imported.length).toBeLessThan(LONG_IMPORT);
    await page.locator('[data-action="import-another"]').click();
    await expect(page.locator("[data-import-upload]")).toBeVisible();
  });
});

test.describe("a ReqIF import past the happy path", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api, [REQIF, SOURCE]));

  test("alice discards a file and stops a long one", async ({ page, api }) => {
    test.setTimeout(90_000);
    must(await api.POST("/projects", { body: { key: REQIF, name: "Exchange" } }));
    await page.goto(`/p/${REQIF}/settings?tab=reqif`);
    const reqif = (name: string, buffer: Buffer) => ({ name, mimeType: "application/xml", buffer });

    const discarded = await upload(page, "ReqIF file", "upload-reqif", reqif("small.reqif", manyObjects(1)));
    await expect(page.locator('[data-reqif-step="schema"]')).toBeVisible();
    await page.locator('[data-action="discard-reqif"]').click();
    await expect(page.locator("[data-reqif-upload]")).toBeVisible();
    expect((await api.GET("/reqif-imports/{importID}", { params: { path: { importID: discarded } } })).response.status).toBe(404);
    expect(await requirementsOf(api, REQIF)).toBe(0);

    const long = await upload(page, "ReqIF file", "upload-reqif", reqif("long.reqif", manyObjects(LONG_IMPORT)));
    await page.locator('[data-action="start-reqif"]').click();
    await page.locator('[data-action="stop-reqif"]').click();
    await expect(page.locator('[data-reqif-step="cancelled"]')).toContainText("Stopped", { timeout: IMPORT_WAIT_MS });
    const found = must(await api.GET("/reqif-imports/{importID}", { params: { path: { importID: long } } })).import;
    expect(found.state).toBe("cancelled");
    expect(found.imported.length).toBeLessThan(LONG_IMPORT);
    await page.locator('[data-action="reqif-another"]').click();
    await expect(page.locator("[data-reqif-upload]")).toBeVisible();
  });

  test("alice downloads a project as .reqif and imports it elsewhere, leaving out what does not fit", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: SOURCE, name: "Exported" } }));
    must(await api.POST("/projects", { body: { key: REQIF, name: "Exchange" } }));
    const source = { params: { path: { projectKey: SOURCE } } };
    const weight = must(await api.POST("/projects/{projectKey}/attributes", { ...source, body: { name: "Weight", kind: "number" } })).attribute;
    must(await api.POST("/projects/{projectKey}/requirements", { ...source, body: { title: "Carry 40 kg", attributes: { [weight.id]: 12.5 } } }));
    const text = must(
      await api.POST("/projects/{projectKey}/attributes", { params: { path: { projectKey: REQIF } }, body: { name: "Weight", kind: "text" } }),
    ).attribute;

    await page.goto(`/p/${SOURCE}/settings?tab=reqif`);
    const download = page.waitForEvent("download");
    await page.locator('[data-action="export-reqif"]').click();
    const file = readFileSync(await (await download).path());

    await page.goto(`/p/${REQIF}/settings?tab=reqif`);
    await upload(page, "ReqIF file", "upload-reqif", { name: `${SOURCE}.reqif`, mimeType: "application/xml", buffer: file });
    await expect(page.locator("[data-reqif-problems]")).toContainText("Weight");
    await expect(page.locator('[data-action="start-reqif"]')).toBeDisabled();
    await page.getByRole("combobox", { name: "What Weight becomes" }).selectOption({ label: "Leave it out" });
    await expect(page.getByText("Nothing keeps the import from starting.")).toBeVisible();
    await page.locator('[data-action="start-reqif"]').click();
    await expect(page.locator('[data-reqif-step="done"]')).toContainText("1 requirement made", { timeout: IMPORT_WAIT_MS });

    const made = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${REQIF}-R1` } } })).requirement;
    expect(made.title).toBe("Carry 40 kg");
    expect((made.attributes as Record<string, unknown>)[text.id]).toBeUndefined();
  });
});

test.describe("a Word import past the happy path", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api, [WORD]));

  test("alice changes the heading pattern, sees the tree follow, and discards the document", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: WORD, name: "From Word" } }));
    await page.goto(`/p/${WORD}/settings?tab=word`);
    const id = await upload(page, "Word document", "upload-word", {
      name: "lamps.docx",
      mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      buffer: wordDocument([{ heading: 1, text: "Lamps" }, { paragraph: "The lamp shall light at night." }, { paragraph: "LMP-7 The lamp dims at dawn." }]),
    });
    const tree = page.locator("[data-word-tree]");
    await expect(tree.locator('[data-word-node="requirement"]')).toHaveCount(1);
    await expect(tree.locator('[data-word-node="informative"]')).toContainText("LMP-7");

    await page.getByLabel("Pattern").fill("LMP-\\d+");
    await expect(tree.locator('[data-word-node="requirement"]')).toHaveCount(2);
    await expect(tree.locator('[data-word-node="informative"]')).toHaveCount(0);
    await expectAccessible(page);

    await page.locator('[data-action="discard-word"]').click();
    await expect(page.locator("[data-word-upload]")).toBeVisible();
    expect((await api.GET("/reqif-imports/{importID}", { params: { path: { importID: id } } })).response.status).toBe(404);
    expect(await requirementsOf(api, WORD)).toBe(0);
  });
});

test.describe("a shared template", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api, [OWNER, OTHER]));

  test("alice offers a template to every project, then deletes it", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: OWNER, name: "Keeps it" } }));
    must(await api.POST("/projects", { body: { key: OTHER, name: "Borrows it" } }));
    const name = `Shared ${OWNER}`;
    await page.goto(`/p/${OWNER}/settings?tab=templates`);
    for (const [key, called] of [
      [OWNER, name],
      [OTHER, "Own"],
    ] as const) {
      const made = await page.request.post(`/api/v1/projects/${key}/export-templates`, {
        multipart: {
          name: called,
          file: {
            name: "t.docx",
            mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            buffer: wordTemplate("Cover", "Header"),
          },
        },
      });
      expect(made.status()).toBe(201);
    }
    const options = async () => {
      await page.goto(`/p/${OTHER}/spec`);
      await page.locator("main").locator('[data-action="export-menu"]').first().click();
      await page.getByRole("menuitem", { name: "Word" }).click();
      const chooser = page.getByRole("dialog").getByRole("combobox", { name: "Template" });
      await expect(chooser).toBeVisible();
      return chooser.locator("option").allTextContents();
    };
    expect(await options()).not.toContain(`${name} (${OWNER})`);

    await page.goto(`/p/${OWNER}/settings?tab=templates`);
    const card = page.locator(`[data-template="${name}"]`);
    await card.getByLabel("Offer it to every project").check();
    await card.locator('[data-action="save-template"]').click();
    await expect(card.getByText("The template is saved.")).toBeVisible();
    expect(await options()).toContain(`${name} (${OWNER})`);

    await page.goto(`/p/${OWNER}/settings?tab=templates`);
    await card.locator('[data-action="delete-template"]').click();
    await expectAccessible(page);
    await page.getByRole("dialog").locator('[data-action="confirm-delete-template"]').click();
    await expect(card).toHaveCount(0);
    expect(await options()).not.toContain(`${name} (${OWNER})`);
  });
});
