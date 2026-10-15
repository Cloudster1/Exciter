import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL, statorURL } from "../fixtures/stack";

// Requirements traced to Stator pages (#41): the stub announces a new version of the page through its webhook, which
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
});
