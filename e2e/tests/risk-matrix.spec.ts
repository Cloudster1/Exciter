import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Mitigations, the FMEA table and the risk matrix (#260): a hazard is mitigated by a requirement, which takes
// effect once it is verified, and the hazard moves to its cell after mitigation.

const KEY = uniqueKey("RMX");
const SIZE = 5;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
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

test.describe("risk matrix", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("a hazard moves to its cell after mitigation once the mitigation is verified", async ({ page, api }) => {
    // It reads the table, the matrix twice and a risk's page, each checked for accessibility.
    test.setTimeout(60_000);
    const path = { params: { path: { projectKey: KEY } } };
    const project = must(await api.POST("/projects", { body: { key: KEY, name: "Brakes" } })).project;
    const hazard = must(
      await api.POST("/projects/{projectKey}/types", { ...path, body: { name: "Hazard", icon: "risk", colour: "#c2410c", role: "risk" } }),
    ).type;
    must(await api.PUT("/projects/{projectKey}/risk-scheme", { ...path, body: scheme() }));
    must(
      await api.PUT("/projects/{projectKey}/coverage-rules", {
        ...path,
        body: { typeId: project.settings.defaultTypeId ?? undefined, decomposed: false, implemented: false, verified: true },
      }),
    );
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Stop on ice" } }));
    must(
      await api.POST("/projects/{projectKey}/requirements", {
        ...path,
        body: {
          title: "Brakes fail on ice",
          typeId: hazard.id,
          risk: { before: { severity: 4, probability: 3, detection: null }, after: { severity: 2, probability: 3, detection: null } },
        },
      }),
    );
    must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: "Check the brakes on ice" } }));
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-R1` } }, body: { typeKey: "mitigates", otherKey: `${KEY}-R2` } }));

    await page.goto(`/projects/${KEY}/risks`);
    const row = page.locator(`[data-fmea-row="${KEY}-R2"]`);
    await expect(row.locator('[data-risk-state="claimed"]')).toBeVisible();
    await expect(row.locator(`[data-mitigation="${KEY}-R1"]`)).toContainText("Not in effect");
    await expect(page.locator('[data-action="export-risks"]')).toHaveAttribute("href", new RegExp(`/projects/${KEY}/risks\\.xlsx`));
    await expectAccessible(page);

    await page.locator('[data-tab="matrix"]').click();
    await expect(page.locator('[data-risk-cell="4:3"]')).toHaveAttribute("data-count", "1");
    await expectAccessible(page);

    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-R3` } }, body: { typeKey: "verifies", otherKey: `${KEY}-R1` } }));
    await page.reload();
    await expect(page.locator('[data-risk-cell="2:3"]')).toHaveAttribute("data-count", "1");
    await page.getByRole("button", { name: "1 risk at severity 2 and probability 3" }).click();
    const risks = page.getByRole("region", { name: "Risks at severity 2 and probability 3" });
    await expect(risks.locator('[data-risk-state="mitigated"]')).toBeVisible();
    await expectAccessible(page);

    await risks.getByRole("link", { name: `${KEY}-R2` }).click();
    await expect(page.getByRole("region", { name: /Mitigations/ }).locator('[data-risk-state="mitigated"]')).toBeVisible();
    await expectAccessible(page);
  });
});
