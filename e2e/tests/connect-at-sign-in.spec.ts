import { must } from "../fixtures/api";
import { anonymousContext, expect, signIn, test } from "../fixtures/auth";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL, statorURL } from "../fixtures/stack";

// Connecting people at sign-in (#274): Exciter is a partner app of both stubs, and alice, signing in through the
// stack's Keycloak, links a requirement to an Armature issue and a Stator page without connecting her accounts.

const KEY = uniqueKey("CSI");
// The connections are the installation's, so the spec runs alone, with a tenant of each stub's of its own.
const TENANT = "e2e-connect-at-sign-in";
const APP = { clientId: `exciter-${TENANT}`, clientSecret: "exciter exchange secret", keycloakClientId: "exciter" };

async function control(base: string, path: string, init: RequestInit = {}): Promise<Response> {
  const res = await fetch(`${base}/_stub/${TENANT}${path}`, { ...init, headers: { "Content-Type": "application/json" } });
  expect(res.ok, `${init.method ?? "GET"} /_stub/${TENANT}${path} answered ${res.status}`).toBe(true);
  return res;
}

interface StubLink {
  createdByName: string;
  issueKey?: string;
}

async function stubLinks(base: string): Promise<StubLink[]> {
  return ((await (await control(base, "/remote-links")).json()) as { remoteLinks: StubLink[] }).remoteLinks;
}

test.describe("connecting alice to Armature and Stator as she signs in", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
    await api.DELETE("/stator/connection");
    for (const base of [armatureURL(), statorURL()]) {
      await fetch(`${base}/_stub/${TENANT}`, { method: "DELETE" });
      await control(base, "/oauth-app", { method: "PUT", body: JSON.stringify(APP) });
    }
    const app = { clientId: APP.clientId, clientSecret: APP.clientSecret };
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), ...app } }));
    must(await api.PUT("/stator/connection", { body: { baseUrl: statorURL(), ...app } }));
  });

  test.afterEach(async ({ api }) => {
    const path = { params: { path: { projectKey: KEY } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
    await api.DELETE("/armature/connection");
    await api.DELETE("/stator/connection");
  });

  test("alice signs in and traces a requirement to an issue and a page, having pasted no token", async ({ browser }) => {
    const context = await anonymousContext(browser);
    const page = await context.newPage();
    await signIn(page, "alice");

    await page.goto("/settings/armature-account");
    await expect(page.getByText("Connected when you signed in.", { exact: false })).toBeVisible();
    await expect(page.locator("[data-armature-token-form]")).toHaveCount(0);
    await expectAccessible(page);
    await page.goto("/settings/stator-account");
    await expect(page.getByText("Connected when you signed in.", { exact: false })).toBeVisible();

    expect((await page.request.post("/api/v1/projects", { data: { key: KEY, name: "Connected at sign-in" } })).ok()).toBe(true);
    expect((await page.request.post(`/api/v1/projects/${KEY}/requirements`, { data: { title: "Brakes engage within 100 ms" } })).ok()).toBe(true);
    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);

    const issues = page.getByRole("region", { name: "Armature issues" });
    const issueForm = issues.getByRole("form", { name: "Trace to an Armature issue" });
    await issueForm.getByLabel("Relation").selectOption("implements");
    await issueForm.getByRole("combobox", { name: "Issue" }).fill("build pipeline");
    await issueForm.getByRole("option", { name: /Set up the build pipeline/ }).click();
    await issueForm.locator('[data-action="create-armature-link"]').click();
    await expect(issues.locator('[data-armature-link="CP-1"]')).toHaveAttribute("data-sync", "synced");

    const pages = page.getByRole("region", { name: "Stator pages" });
    const pageForm = pages.getByRole("form", { name: "Link a Stator page" });
    await pageForm.getByLabel("Relation").selectOption("specifies");
    await pageForm.getByRole("combobox", { name: "Page" }).fill("brake tim");
    await pageForm.getByRole("option", { name: /Brake timing/ }).click();
    await pageForm.locator('[data-action="create-stator-link"]').click();
    await expect(pages.getByRole("link", { name: /Brake timing/ })).toBeVisible();

    // The links on the issue and the page were put there as alice, with the tokens her sign-in exchanged.
    await expect.poll(async () => (await stubLinks(armatureURL())).find((l) => l.issueKey === "CP-1")?.createdByName).toBe("Alice");
    // The tenant is the spec's own, so the one link on its pages is the one alice made.
    await expect.poll(async () => (await stubLinks(statorURL()))[0]?.createdByName).toBe("Alice");
    await context.close();
  });
});
