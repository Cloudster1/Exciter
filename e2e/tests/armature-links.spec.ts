import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { openShowing } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Requirements traced to Armature issues (#27): the stack's own worker puts the link on the stub's issue.

const KEY = uniqueKey("ALK");
// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-armature-links";
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;

interface StubLink {
  issueKey: string;
  url: string;
  title: string;
  source: string;
  createdByName: string;
}

async function stubLinks(tenant: string): Promise<StubLink[]> {
  const res = await fetch(`${armatureURL()}/_stub/${tenant}/remote-links`);
  return ((await res.json()) as { remoteLinks: StubLink[] }).remoteLinks;
}

test.describe("tracing requirements to Armature issues", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(TENANT, "alice") } }));
    must(await api.POST("/projects", { body: { key: KEY, name: "Armature links" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));
  });

  test.afterEach(async ({ api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
    await api.DELETE("/armature/connection");
  });

  test("alice traces a requirement to an issue she finds by its summary, and Armature shows it from Exciter", async ({ page }) => {
    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Armature issues" });
    await expect(panel.getByText(/No issue yet/)).toBeVisible();

    const form = panel.getByRole("form", { name: "Trace to an Armature issue" });
    await form.getByLabel("Relation").selectOption("implements");
    await form.getByRole("combobox", { name: "Issue" }).fill("build pipeline");
    await form.getByRole("option", { name: /Set up the build pipeline/ }).click();
    await form.locator('[data-action="create-armature-link"]').click();

    const link = panel.locator('[data-armature-link="CP-1"]');
    await expect(link.getByRole("link", { name: /Set up the build pipeline/ })).toBeVisible();
    await expect(link).toHaveAttribute("data-sync", "synced");
    await expectAccessible(page);

    const held = (await stubLinks(TENANT)).filter((l) => l.issueKey === "CP-1");
    expect(held).toHaveLength(1);
    expect(held[0]).toMatchObject({ source: "Exciter", title: `${KEY}-R1 Brakes engage within 100 ms (implements)`, createdByName: "Alice" });
    expect(held[0]!.url).toMatch(/\/r\/[0-9a-f-]{36}$/);

    await panel.getByRole("button", { name: "Remove CP-1, Implemented by the issue" }).click();
    await expect(panel.getByText(/No issue yet/)).toBeVisible();
    await expect.poll(async () => (await stubLinks(TENANT)).filter((l) => l.issueKey === "CP-1").length).toBe(0);
  });

  test("alice confirms a trace her revision left suspect (#326)", async ({ page, api }) => {
    const reqKey = `${KEY}-R1`;
    must(await api.POST("/requirements/{reqKey}/armature-links", { params: { path: { reqKey } }, body: { issueKey: "CP-1", relation: "implements" } }));
    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey } }, body: { title: "Brakes engage within 80 ms", baseRevision: 1 } }));
    await page.goto(`/projects/${KEY}/r/${reqKey}`);
    const link = page.getByRole("region", { name: "Armature issues" }).locator('[data-armature-link="CP-1"]');
    await expect(link.getByText("Suspect")).toBeVisible();

    await link.getByRole("button", { name: "Check the trace to CP-1" }).click();
    const dialog = page.getByRole("dialog", { name: `Check the trace of ${reqKey} to CP-1` });
    await expect(dialog).toBeVisible();
    await expectAccessible(page);
    await dialog.getByLabel("Why the link still holds").fill("The pipeline still builds what the requirement asks.");
    await dialog.getByRole("button", { name: "Confirm link" }).click();
    await expect(dialog).toBeHidden();
    await expect(link.getByText("Suspect")).toBeHidden();
    await page.reload();
    await expect(link.getByRole("link", { name: /Set up the build pipeline/ })).toBeVisible();
    await expect(link.getByText("Suspect")).toBeHidden();
    const stored = must(await api.GET("/requirements/{reqKey}/armature-links", { params: { path: { reqKey } } })).links;
    expect(stored).toMatchObject([{ issueKey: "CP-1", suspect: false, reviewedRevision: 2 }]);
  });

  test("bob, without a token, follows a chip's hint to connect, and then sees the issue live (#326)", async ({ api, pageAs }) => {
    const reqKey = `${KEY}-R1`;
    must(await api.POST("/requirements/{reqKey}/armature-links", { params: { path: { reqKey } }, body: { issueKey: "CP-1", relation: "implements" } }));
    const bob = await pageAs("bob");
    const chip = bob.locator('[data-armature-link="CP-1"] [data-armature-issue="CP-1"]');
    await openShowing(bob, `/projects/${KEY}/r/${reqKey}`, chip);
    await expect(chip).toHaveAttribute("data-state", "connect");
    await expect(chip.getByRole("link", { name: "CP-1", exact: true })).toBeVisible();
    await expect(chip.locator('[data-status-category="done"]')).toHaveText(/Done/);
    await expect(chip).not.toContainText("Set up the build pipeline");
    // Armature moves the issue on, so the chip shows its status now only when it asks Armature.
    const moved = await fetch(`${armatureURL()}/_stub/${TENANT}/issues/CP-1`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ statusCategory: "in_progress" }),
    });
    expect(moved.status).toBe(200);

    await chip.locator('[data-action="connect-armature-hint"]').click();
    await expect(bob).toHaveURL(/\/settings\/armature-account$/);
    await bob.getByLabel("Armature token", { exact: true }).fill(patFor(TENANT, "bob"));
    await bob.locator('[data-action="connect-armature"]').click();
    await expect(bob.locator("[data-armature-user]")).toContainText("Connected as Bob");

    await bob.goto(`/projects/${KEY}/r/${reqKey}`);
    const live = bob.locator('[data-armature-link="CP-1"] [data-armature-issue="CP-1"]');
    await expect(live).toHaveAttribute("data-state", "issue");
    await expect(live).toContainText("Set up the build pipeline");
    await expect(live).toContainText("Alice");
    await expect(live.locator("[data-status-category]")).toHaveAttribute("data-status-category", "in_progress");
    await expect(live.locator("[data-status-category]")).toHaveText(/In progress/);
  });

  test("alice picks another issue, and one she may not see is never offered to trace (#326)", async ({ page, api }) => {
    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Armature issues" });
    const form = panel.getByRole("form", { name: "Trace to an Armature issue" });
    const box = form.getByRole("combobox", { name: "Issue" });
    const create = form.locator('[data-action="create-armature-link"]');
    await box.fill("build pipeline");
    await form.getByRole("option", { name: /Set up the build pipeline/ }).click();
    await expect(form.locator('[data-armature-picked="CP-1"]')).toBeVisible();
    await expect(box).toBeHidden();
    await expect(create).toBeEnabled();

    await form.locator('[data-action="pick-another-issue"]').click();
    await expect(form.locator("[data-armature-picked]")).toHaveCount(0);
    await expect(box).toBeVisible();
    await expect(create).toBeDisabled();

    // SEC-1 is the stub's admin's alone, so Armature answers alice's search for it with nothing.
    await box.fill("SEC-1");
    await expect(form.getByText("No issue that you may see matches. Try its key, or other words.")).toBeVisible();
    await expect(form.locator("[data-issue-option]")).toHaveCount(0);
    await box.press("Enter");
    await expect(create).toBeDisabled();
    await expect(panel.getByText(/No issue yet/)).toBeVisible();
    const stored = must(await api.GET("/requirements/{reqKey}/armature-links", { params: { path: { reqKey: `${KEY}-R1` } } })).links;
    expect(stored).toEqual([]);
  });

  test("alice opens a traced issue in Armature from the requirement's trace graph (#314)", async ({ page, api }) => {
    must(
      await api.POST("/requirements/{reqKey}/armature-links", {
        params: { path: { reqKey: `${KEY}-R1` } },
        body: { issueKey: "CP-1", relation: "implements" },
      }),
    );
    await page.goto(`/projects/${KEY}/r/${KEY}-R1/trace`);
    await page.locator("[data-graph-canvas] [data-graph-node]", { hasText: "CP-1" }).click();
    const chosen = page.locator('[data-graph-chosen="CP-1"]');
    await expect(chosen).toContainText("Set up the build pipeline");
    const open = chosen.locator('[data-action="graph-open"]');
    await expect(open).toHaveAttribute("target", "_blank");
    await expect(open).toHaveAttribute("href", /CP-1/);
    const opened = page.waitForEvent("popup");
    await open.click();
    expect((await opened).url()).toContain("CP-1");
  });
});
