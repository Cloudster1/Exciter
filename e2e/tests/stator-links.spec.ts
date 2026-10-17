import { readFile } from "node:fs/promises";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL, statorURL } from "../fixtures/stack";

// Requirements traced to Stator pages (#41), and read as the matrix's columns (#150): the stub announces a new version of the page through its webhook, which
// the stack receives as it would Stator's.

const KEY = uniqueKey("SLK");
// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-stator-links";
const WEBHOOK_SECRET = "stator_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";

async function stub(path: string, init: RequestInit = {}): Promise<Response> {
  const res = await fetch(`${statorURL()}/_stub/${TENANT}${path}`, { ...init, headers: { "Content-Type": "application/json" } });
  expect(res.ok, `${init.method ?? "GET"} /_stub/${TENANT}${path} answered ${res.status}`).toBe(true);
  return res;
}

async function pageId(title: string): Promise<string> {
  const { pages } = (await (await stub("/pages")).json()) as { pages: { id: string; title: string }[] };
  return pages.find((p) => p.title === title)!.id;
}

test.describe("tracing requirements to Stator pages", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${statorURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/stator/connection", { body: { baseUrl: statorURL(), webhookSecret: WEBHOOK_SECRET } }));
    must(await api.PUT("/stator/account/token", { body: { token: `stator_pat_${TENANT}_alice` } }));
    await stub("/webhook", { method: "PUT", body: JSON.stringify({ url: `${WEB_URL}/api/v1/stator/webhook`, secret: WEBHOOK_SECRET }) });
    must(await api.POST("/projects", { body: { key: KEY, name: "Stator links" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));
  });

  test.afterEach(async ({ api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
    await api.DELETE("/stator/connection");
  });

  test("alice links a page she finds by its title, and confirms the link once Stator publishes a new version", async ({ page }) => {
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Stator pages" });
    await expect(panel.getByText(/No page yet/)).toBeVisible();

    const form = panel.getByRole("form", { name: "Link a Stator page" });
    await form.getByLabel("Relation").selectOption("specifies");
    await form.getByRole("combobox", { name: "Page" }).fill("brake tim");
    await form.getByRole("option", { name: /Brake timing/ }).click();
    await form.locator('[data-action="create-stator-link"]').click();

    const timing = await pageId("Brake timing");
    const link = panel.locator(`[data-stator-link="${timing}"]`);
    await expect(link.getByRole("link", { name: /Brake timing/ })).toContainText("Version 2");
    await expectAccessible(page);

    // Somebody publishes the page in Stator; its webhook tells Exciter, and the link waits to be checked.
    await stub(`/pages/${timing}/publish`, { method: "POST", body: JSON.stringify({}) });
    await expect
      .poll(async () => {
        await page.reload();
        await link.getByRole("link", { name: /Brake timing/ }).waitFor();
        return link.getByText("Suspect").isVisible();
      })
      .toBe(true);
    await link.getByRole("button", { name: "Check the trace to Brake timing" }).click();
    const dialog = page.getByRole("dialog", { name: `Check the trace of ${KEY}-R1 to Brake timing` });
    await expect(dialog).toContainText("from version 2 to version 3");
    await dialog.getByLabel("Why the link still holds").fill("The timing table still holds.");
    await dialog.getByRole("button", { name: "Confirm link" }).click();
    await expect(dialog).toBeHidden();
    await expect(link.getByText("Suspect")).toBeHidden();
    await expectAccessible(page);

    await panel.getByRole("button", { name: "Remove Brake timing, Specified by the page" }).click();
    await expect(panel.getByText(/No page yet/)).toBeVisible();
  });

  // The matrix (#150): the pages the requirements are traced to as its columns, titled as alice reads them.
  test("alice reads the pages as matrix columns and opens a page's cell", async ({ page, api }) => {
    const timing = await pageId("Brake timing");
    must(await api.POST("/requirements/{reqKey}/stator-links", { params: { path: { reqKey: `${KEY}-R1` } }, body: { pageId: timing, relation: "describes" } }));

    await page.goto(`/p/${KEY}/matrix?columns=pages`);
    const matrix = page.getByRole("table", { name: "Matrix" });
    await expect(matrix.getByRole("columnheader", { name: "Brake timing" })).toBeVisible();
    await page.getByRole("button", { name: `${KEY}-R1 to Brake timing: describes` }).click();
    const cell = page.getByRole("dialog", { name: `${KEY}-R1 and Brake timing` });
    await expect(cell.getByText("Described by Brake timing", { exact: true })).toBeVisible();
    await expectAccessible(page);
    await page.keyboard.press("Escape");

    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download CSV" }).click();
    const file = await readFile((await (await download).path()) ?? "", "utf8");
    expect(file).toContain("Key,Title,Links,Brake timing");
  });

  // The way back (#42): the worker puts the requirement on the page as alice, and a person taking it off in Stator
  // is told to Exciter through the stub's webhook.
  test("a page alice links shows the requirement in Stator, and a link taken off there stays off", async ({ page, api }) => {
    const requirement = must(await api.GET("/requirements/{reqKey}", { params: { path: { reqKey: `${KEY}-R1` } } })).requirement;
    const timing = await pageId("Brake timing");
    must(await api.POST("/requirements/{reqKey}/stator-links", { params: { path: { reqKey: `${KEY}-R1` } }, body: { pageId: timing, relation: "describes" } }));
    const remote = async () => {
      const { remoteLinks } = (await (await stub("/remote-links")).json()) as { remoteLinks: { pageId: string; url: string; title: string }[] };
      return remoteLinks.find((l) => l.pageId === timing && l.url.endsWith(`/r/${requirement.id}`));
    };
    await expect.poll(async () => (await remote())?.title, { timeout: 30_000 }).toBe(`${KEY}-R1 Brakes engage within 100 ms (describes)`);

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const link = page.getByRole("region", { name: "Stator pages" }).locator(`[data-stator-link="${timing}"]`);
    await expect(link).toHaveAttribute("data-sync", "synced");
    await expect(link.getByText("Waiting for Stator")).toBeHidden();

    const url = (await remote())!.url;
    await stub(`/pages/${timing}/remote-links?url=${encodeURIComponent(url)}`, { method: "DELETE" });
    await expect
      .poll(async () => {
        await page.reload();
        await link.waitFor();
        return link.getByText("Removed in Stator").isVisible();
      })
      .toBe(true);
    await expectAccessible(page);
  });
});
