import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
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
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
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

  test("alice opens a traced issue in Armature from the requirement's trace graph (#314)", async ({ page, api }) => {
    must(
      await api.POST("/requirements/{reqKey}/armature-links", {
        params: { path: { reqKey: `${KEY}-R1` } },
        body: { issueKey: "CP-1", relation: "implements" },
      }),
    );
    await page.goto(`/p/${KEY}/r/${KEY}-R1/trace`);
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
