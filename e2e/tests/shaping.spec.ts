import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect, type User } from "../fixtures/auth";
import { peopleTest as test, type Person } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, toast, dismissToasts } from "../fixtures/shell";
import { setProjectGrants } from "../fixtures/grants";

// Shaping a project in its settings (#322): every control of the types, attributes, field rules, statuses and the
// workflow designer is used, and what it changed is checked where a requirement meets it.

const KEY = uniqueKey("SHP");
const project = { params: { path: { projectKey: KEY } } };

async function removeProject(api: ExciterApi) {
  const found = await api.GET("/projects/{projectKey}", project);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", project);
  await api.DELETE("/projects/{projectKey}", project);
}

/** The project, with alice its administrator and bob an editor. */
async function makeProject(api: ExciterApi, people: Record<User, Person>) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Shaped" } }));
  await setProjectGrants(api, KEY, [
    { subject: { type: "user", id: people.alice.id }, role: "admin" },
    { subject: { type: "user", id: people.bob.id }, role: "editor" },
  ]);
}

async function statusIds(api: ExciterApi) {
  const statuses = must(await api.GET("/projects/{projectKey}/statuses", project)).statuses;
  return Object.fromEntries(statuses.map((status) => [status.name, status.id]));
}

test.describe("shaping a project", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice renames, retires and deletes types, and the requirement form offers what is left", async ({ page, api, people }) => {
    await makeProject(api, people);
    const typeOf = (name: string, icon: string) => api.POST("/projects/{projectKey}/types", { ...project, body: { name, icon, colour: "#4f6bed" } });
    for (const [name, icon] of [
      ["Interface", "interface"],
      ["Constraint", "constraint"],
      ["Spare", "data"],
    ]) {
      must(await typeOf(name!, icon!));
    }
    const legacy = must(await typeOf("Legacy", "need")).type;
    // A requirement that was Legacy at revision 1 keeps the type in its history, though no requirement is Legacy now.
    const written = must(
      await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Pay the basket", typeId: legacy.id } }),
    ).requirement;
    const fallback = must(await api.GET("/projects/{projectKey}", project)).project.settings.defaultTypeId!;
    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: written.key } }, body: { typeId: fallback, baseRevision: 1 } }));

    await page.goto(`/projects/${KEY}/settings/types`);
    await page.locator('[data-type="Interface"] [data-action="edit-type"]').click();
    const form = page.locator("[data-type-form]");
    await form.getByLabel("Name", { exact: true }).fill("Port");
    await form.locator('[data-action="save-type"]').click();
    await expect(page.locator('[data-type="Port"]')).toBeVisible();
    await expect(page.locator('[data-type="Interface"]')).toHaveCount(0);

    await page.locator('[data-type="Constraint"] [data-action="retire-type"]').click();
    await expect(page.locator('[data-type="Constraint"]')).toHaveAttribute("data-retired", "");

    await page.locator('[data-type="Spare"] [data-action="delete-type"]').click();
    await expect(page.locator('[data-type="Spare"]')).toHaveCount(0);
    await page.locator('[data-type="Legacy"] [data-action="delete-type"]').click();
    await expect(page.locator("[data-type-list]")).toContainText(
      "This type is used by 1 requirements and revisions. Retire it instead, so their history keeps it.",
    );
    await expect(page.locator('[data-type="Legacy"]')).toBeVisible();
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/new`);
    const select = page.locator("select[data-type-select]");
    await expect(select.locator("option")).toHaveText(["Requirement", "Port", "Legacy"]);
    await select.selectOption({ label: "Port" });
    await page.getByLabel("Title", { exact: true }).fill("Talk to the payment gateway");
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page.locator('[data-requirement-type="Port"]')).toBeVisible();
  });

  test("alice adds an option, deletes an attribute and moves one up, and the requirement form follows", async ({ page, api, people }) => {
    await makeProject(api, people);
    const attributes = { ...project };
    must(await api.POST("/projects/{projectKey}/attributes", { ...attributes, body: { name: "Verification", kind: "select", options: [{ name: "Test" }] } }));
    must(await api.POST("/projects/{projectKey}/attributes", { ...attributes, body: { name: "Notes", kind: "text" } }));
    must(await api.POST("/projects/{projectKey}/attributes", { ...attributes, body: { name: "Unused", kind: "boolean" } }));

    await page.goto(`/projects/${KEY}/settings/attributes`);
    await page.locator('[data-attribute="Verification"] [data-action="edit-attribute"]').click();
    const form = page.locator("[data-attribute-form]");
    await form.getByLabel("New option").fill("Inspection");
    await form.locator('[data-action="add-option"]').click();
    await form.locator('[data-action="save-attribute"]').click();
    await expect(page.locator("[data-attribute-form]")).toHaveCount(0);

    await page.locator('[data-attribute="Unused"] [data-action="delete-attribute"]').click();
    await expect(page.locator('[data-attribute="Unused"]')).toHaveCount(0);
    await page.getByRole("button", { name: "Move Notes up" }).click();
    await expect
      .poll(() => page.locator("[data-attribute]").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-attribute"))))
      .toEqual(["Notes", "Verification"]);
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/new`);
    await expect(page.locator("[data-attribute-field]")).toHaveCount(2);
    await expect(page.locator("[data-attribute-field]").first()).toHaveAttribute("data-attribute-field", "Notes");
    await page.getByLabel("Title", { exact: true }).fill("Inspect the brakes yearly");
    await page.getByLabel("Verification", { exact: true }).selectOption({ label: "Inspection" });
    await page.locator('[data-action="create-requirement"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(page.locator("[data-attribute-values]")).toContainText("Inspection");
  });

  test("alice sets field rules, and a transition and bob's edit are held to them", async ({ page, api, people, pageAs }) => {
    await makeProject(api, people);
    must(await api.POST("/projects/{projectKey}/attributes", { ...project, body: { name: "Risk", kind: "text" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(`/projects/${KEY}/settings/attributes`);
    await page.locator('[data-attribute="Risk"] [data-action="edit-attribute"]').click();
    const form = page.locator("[data-attribute-form]");
    await form.getByLabel("Required from").selectOption({ label: "In review" });
    await form.locator('[data-action="add-permit"]').click();
    const line = form.locator('[data-permit="1"]');
    await line.getByLabel("Project administrator", { exact: true }).check();
    await line.getByLabel("Draft", { exact: true }).check();
    await expectAccessible(page);
    await form.locator('[data-action="save-attribute"]').click();
    await expect(page.locator('[data-attribute="Risk"]')).toBeVisible();
    await expect(page.locator("[data-attribute-form]")).toHaveCount(0);

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const bar = page.locator("[data-transition-bar]");
    await bar.getByRole("button", { name: "Submit for review" }).click();
    await expect(bar).toContainText("Give Risk a value; requirements in In review need it.");
    await expect(bar.locator('[data-status="Draft"]')).toBeVisible();

    const bob = await pageAs("bob");
    await openUntil(bob, `/projects/${KEY}/r/${KEY}-R1`, () => expect(bob.locator('[data-action="edit-requirement"]')).toBeVisible(ONE_LOOK));
    await bob.locator('[data-action="edit-requirement"]').click();
    await expect(bob.locator('[data-attribute-field="Risk"]')).toContainText("Only admins change Risk in Draft.");

    await page.goto(`/projects/${KEY}/settings/fields`);
    const owner = page.locator('[data-fixed-field="owner"]');
    await owner.getByLabel("Required from").selectOption({ label: "In review" });
    await owner.locator('[data-action="save-field-rule"]').click();
    await expect(toast(page, "The rule of Owner is saved.")).toBeVisible();
    await dismissToasts(page);
    await expectAccessible(page);

    const riskId = must(await api.GET("/projects/{projectKey}/attributes", project)).attributes[0]!.id;
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes hold on a slope", attributes: { [riskId]: "low" } } }));
    await page.goto(`/projects/${KEY}/r/${KEY}-R2`);
    await bar.getByRole("button", { name: "Submit for review" }).click();
    await expect(bar).toContainText("Give the owner a value");
    await expect(bar.locator('[data-status="Draft"]')).toBeVisible();
  });

  test("alice makes and deletes a status, and one a workflow names stays", async ({ page, api, people }) => {
    await makeProject(api, people);
    await page.goto(`/projects/${KEY}/settings/workflow`);
    await page.locator('[data-action="new-status"]').click();
    const coin = page.locator("[data-status-list] [data-new-status]");
    await coin.getByLabel("Status name").fill("Parked");
    await coin.getByLabel("Counts as").selectOption({ label: "Closed" });
    await coin.locator('[data-action="create-status"]').click();
    await expect(page.locator('[data-status-row="Parked"]')).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-status-row="Parked"] [data-action="delete-status"]').click();
    await expect(page.locator('[data-status-row="Parked"]')).toHaveCount(0);
    // No requirement is Obsolete, but the default workflow names it.
    await page.locator('[data-status-row="Obsolete"] [data-action="delete-status"]').click();
    await expect(page.locator("[data-status-list]")).toContainText("This status is still used once. Assign another in its place first.");
    await expect(page.locator('[data-status-row="Obsolete"]')).toBeVisible();
    expect(Object.keys(await statusIds(api))).toContain("Obsolete");
  });

  test("alice draws a workflow in the inspector, and new requirements start where she said", async ({ page, api, people }) => {
    await makeProject(api, people);
    await page.goto(`/projects/${KEY}/settings/workflow`);
    await page.locator('[data-action="new-workflow"]').click();
    await page.getByLabel("Name", { exact: true }).fill("Lean");
    for (const status of ["Draft", "In review", "Approved"]) {
      await page.getByLabel("Add a status").selectOption({ label: status });
      await page.locator('[data-action="add-status"]').click();
    }
    const node = (name: string) => page.locator(`[data-workflow-node="${name}"]`);
    await expect(node("Draft").locator("[data-workflow-initial]")).toBeVisible();

    await node("In review").click();
    await page.locator('[data-action="make-initial"]').click();
    await expect(node("In review").locator("[data-workflow-initial]")).toBeVisible();
    await expect(node("Draft").locator("[data-workflow-initial]")).toHaveCount(0);
    await page.getByLabel("Add a transition to").selectOption({ label: "Approved" });
    await page.locator('[data-action="add-transition"]').click();
    await page.getByLabel("Transition name").fill("Pass");
    await expect(page.locator('[data-workflow-edge-label="Pass"]')).toBeVisible();
    await page.locator('[data-action="remove-transition"]').click();
    await expect(page.locator('[data-workflow-edge-label="Pass"]')).toHaveCount(0);

    await node("Draft").click();
    await page.getByLabel("Add a transition to").selectOption({ label: "In review" });
    await page.locator('[data-action="add-transition"]').click();
    await page.getByLabel("Transition name").fill("Ask");
    await expect(page.locator('[data-workflow-edge-label="Ask"]')).toBeVisible();

    await node("Approved").click();
    await page.locator('[data-action="remove-status"]').click();
    await expect(node("Approved")).toHaveCount(0);
    const coin = page.locator("[data-new-status]");
    await coin.getByLabel("Status name").fill("Parked");
    await coin.locator('[data-action="create-status"]').click();
    await expect(node("Parked")).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-action="save-workflow"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}/settings/workflow`);
    await expect(page.locator('[data-workflow-card="Lean"]')).toContainText("3 statuses, 1 transition");
    await expect(page.locator('[data-status-row="Parked"]')).toBeVisible();

    await page.getByLabel("Workflow of Requirement").selectOption({ label: "Lean" });
    await page.getByRole("dialog", { name: "Follow Lean" }).locator('[data-action="confirm-assign"]').click();
    await expect(page.locator('[data-workflow-card="Lean"]')).toContainText("Followed by Requirement.");
    const made = must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } })).requirement;
    await page.goto(`/projects/${KEY}/r/${made.key}`);
    await expect(page.locator('[data-transition-bar] [data-status="In review"]')).toBeVisible();
  });

  test("alice deletes a workflow nobody follows, and one followed meanwhile stays", async ({ page, api, people }) => {
    await makeProject(api, people);
    const draft = (await statusIds(api))["Draft"]!;
    const workflow = (name: string) =>
      api.POST("/projects/{projectKey}/workflows", { ...project, body: { name, steps: [{ statusId: draft, isInitial: true }], transitions: [] } });
    must(await workflow("Spare"));
    const busy = must(await workflow("Busy")).workflow;

    await page.goto(`/projects/${KEY}/settings/workflow`);
    await page.getByRole("button", { name: "Delete Spare" }).click();
    await expect(page.locator('[data-workflow-card="Spare"]')).toHaveCount(0);

    // Somebody gives Busy to a type while alice still has the page open.
    await expect(page.getByRole("button", { name: "Delete Busy" })).toBeVisible();
    const fallback = must(await api.GET("/projects/{projectKey}", project)).project.settings.defaultTypeId!;
    must(await api.PUT("/projects/{projectKey}/workflow", { ...project, body: { workflowId: busy.id, typeId: fallback } }));
    await page.getByRole("button", { name: "Delete Busy" }).click();
    await expect(page.getByText("This workflow is still used once. Assign another in its place first.")).toBeVisible();
    await expect(page.locator('[data-workflow-card="Busy"]')).toBeVisible();
    await expectAccessible(page);
  });
});
