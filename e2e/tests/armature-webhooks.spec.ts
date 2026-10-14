import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { armatureURL } from "../fixtures/stack";

// Armature's webhooks (#29): the stub posts to the address Exciter shows, signed with the secret stored there, and
// the requirement page follows what changed in Armature.

const KEY = uniqueKey("AWH");
// The connection is the installation's, so the spec runs alone, with a tenant of the stub's of its own.
const TENANT = "e2e-armature-webhooks";
const SECRET = `armature_whs_${"e2e".repeat(14)}x`;
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
// A webhook writes as nobody, so a page read may reach a replica that has not replayed it yet; it is read again.
const REPLAY_WAIT_MS = 1000;

async function stub(method: string, path: string, body?: unknown): Promise<Response> {
  const res = await fetch(`${armatureURL()}/_stub/${TENANT}${path}`, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  expect(res.ok, `${method} /_stub/${TENANT}${path} answered ${res.status}`).toBe(true);
  return res;
}

async function lastDelivery(): Promise<{ topic: string; status: number }> {
  const { deliveries } = (await (await stub("GET", "/deliveries")).json()) as { deliveries: { topic: string; status: number }[] };
  return deliveries[deliveries.length - 1]!;
}

test.describe("Armature's webhooks", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ api }) => {
    await fetch(`${armatureURL()}/_stub/${TENANT}`, { method: "DELETE" });
    const connection = must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), webhookSecret: SECRET } })).connection;
    must(await api.PUT("/armature/account/token", { body: { token: patFor(TENANT, "alice") } }));
    must(await api.POST("/projects", { body: { key: KEY, name: "Armature webhooks" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));
    must(
      await api.POST("/requirements/{reqKey}/armature-links", {
        params: { path: { reqKey: `${KEY}-R1` } },
        body: { issueKey: "CP-4", relation: "implements" },
      }),
    );
    await stub("PUT", "/webhook", { url: connection.webhookUrl, secret: SECRET });
  });

  test.afterEach(async ({ api }) => {
    await stub("PUT", "/webhook", { url: "" });
    const path = { params: { path: { projectKey: KEY } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (found.data) {
      if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
      await api.DELETE("/projects/{projectKey}", path);
    }
    await api.DELETE("/armature/connection");
  });

  test("a requirement follows its issue as it moves on, loses its link and goes", async ({ page }) => {
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.getByRole("region", { name: "Armature issues" });
    const link = panel.locator('[data-armature-link="CP-4"]');
    await expect(link.locator('[data-status-category="todo"]')).toBeVisible();
    await expect(link).toHaveAttribute("data-sync", "synced");

    await stub("PATCH", "/issues/CP-4", { statusCategory: "in_progress" });
    expect(await lastDelivery()).toMatchObject({ topic: "issue.transitioned", status: 204 });
    await page.reload();
    await expect(link.locator('[data-status-category="in_progress"]')).toBeVisible();

    const url = new URL(page.url());
    const held = (await (await stub("GET", "/remote-links")).json()) as { remoteLinks: { issueKey: string; url: string }[] };
    const ours = held.remoteLinks.find((l) => l.issueKey === "CP-4");
    expect(ours?.url).toContain("/r/");
    await stub("DELETE", `/issues/CP-4/remote-links?url=${encodeURIComponent(ours!.url)}`);
    expect(await lastDelivery()).toMatchObject({ topic: "issue.remote_link.removed", status: 204 });
    await expect(async () => {
      await page.goto(url.toString());
      await expect(link.getByText("Removed in Armature")).toBeVisible({ timeout: REPLAY_WAIT_MS });
    }).toPass();

    await stub("DELETE", "/issues/CP-4");
    expect(await lastDelivery()).toMatchObject({ topic: "issue.deleted", status: 204 });
    await expect(async () => {
      await page.reload();
      await expect(link.getByText("Deleted in Armature")).toBeVisible({ timeout: REPLAY_WAIT_MS });
    }).toPass();
  });
});
