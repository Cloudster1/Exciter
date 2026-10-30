import type { Page } from "@playwright/test";
import { must, type ExciterApi } from "./api";
import { confirm } from "./shell";

/** A subject as specs name it: a person, a group, or everyone, which is the Everyone group. */
export type GrantSubject = { type: "everyone" } | { type: "user" | "group"; id: string };

/** One row of a project's grants as the table of before held it: a subject and the role it holds there. */
export interface GrantRow {
  subject: GrantSubject;
  role: string;
}

/** The id of the group that holds every member. */
export async function everyoneGroup(api: ExciterApi): Promise<string> {
  const { groups } = must(await api.GET("/groups", { params: { query: { limit: 50 } } }));
  const everyone = groups.find((group) => group.builtin === "everyone");
  if (!everyone) throw new Error("The installation has no Everyone group.");
  return everyone.id;
}

/**
 * Makes a project's grants the table given, through the API as the caller: each row is granted, then every other
 * grant in the project goes, the caller's own last, so the project keeps somebody to run it throughout.
 */
export async function setProjectGrants(api: ExciterApi, projectKey: string, rows: GrantRow[]): Promise<void> {
  const me = must(await api.GET("/auth/me")).user.id;
  const everyone = rows.some((row) => row.subject.type === "everyone") ? await everyoneGroup(api) : "";
  const { assignments } = must(await api.GET("/role-assignments", { params: { query: { project: projectKey, limit: 200 } } }));
  const kept: string[] = [];
  for (const row of rows) {
    const subject =
      row.subject.type === "everyone" ? { groupId: everyone } : row.subject.type === "group" ? { groupId: row.subject.id } : { userId: row.subject.id };
    const made = must(
      await api.POST("/role-assignments", { body: { role: row.role, projectKey, userId: subject.userId ?? null, groupId: subject.groupId ?? null } }),
    ).assignment;
    kept.push(made.id);
  }
  const gone = assignments.filter((each) => !kept.includes(each.id));
  for (const each of [...gone.filter((g) => g.userId !== me), ...gone.filter((g) => g.userId === me)]) {
    const answer = await api.DELETE("/role-assignments/{assignmentID}", { params: { path: { assignmentID: each.id } } });
    if (!answer.response.ok) throw new Error(`Taking ${each.roleName} from ${each.userName ?? each.groupName} answered ${answer.response.status}.`);
  }
}

/** Who an installation-wide grant names: a person or a group, by id. */
export type Holder = { userId: string } | { groupId: string };

const holderOf = (holder: Holder) => ("userId" in holder ? { userId: holder.userId, groupId: null } : { userId: null, groupId: holder.groupId });

/** Grants a role across the whole installation, unless the holder has it there already. */
export async function grantEverywhere(api: ExciterApi, role: string, holder: Holder): Promise<void> {
  const { assignments } = must(await api.GET("/role-assignments", { params: { query: { scope: "installation", role, limit: 200 } } }));
  const { userId, groupId } = holderOf(holder);
  if (assignments.some((each) => (userId && each.userId === userId) || (groupId && each.groupId === groupId))) return;
  must(await api.POST("/role-assignments", { body: { role, projectKey: "", userId, groupId } }));
}

/** Takes a role across the whole installation from the holder; one they do not hold is fine. */
export async function revokeEverywhere(api: ExciterApi, role: string, holder: Holder): Promise<void> {
  const { assignments } = must(await api.GET("/role-assignments", { params: { query: { scope: "installation", role, limit: 200 } } }));
  const { userId, groupId } = holderOf(holder);
  for (const each of assignments.filter((a) => (userId && a.userId === userId) || (groupId && a.groupId === groupId))) {
    const answer = await api.DELETE("/role-assignments/{assignmentID}", { params: { path: { assignmentID: each.id } } });
    if (!answer.response.ok) throw new Error(`Taking ${each.roleName} from ${each.userName ?? each.groupName} answered ${answer.response.status}.`);
  }
}

/** Grants a role through the grant form on the page: who by the name the picker offers, the role by its key, and where when the form asks. */
export async function grantInForm(page: Page, who: string, role: string, where?: string): Promise<void> {
  const form = page.locator("[data-grant-form]");
  await form.locator("[data-subject-picker] input").fill(who);
  await form.locator(`[data-subject-option="${who}"]`).click();
  await form.locator("[data-grant-role-choice]").selectOption(role);
  if (where !== undefined) await form.locator("[data-grant-where]").selectOption(where);
  await form.locator('[data-action="grant-role"]').click();
}

/** The row of the grants table on the page that says who holds which role, by its key. */
export const grantRow = (page: Page, who: string, role: string) => page.locator(`[data-grant="${who}"][data-grant-role="${role}"]`);

/** Revokes a grant from the table on the page, answering its question. */
export async function revokeInTable(page: Page, who: string, role: string): Promise<void> {
  await grantRow(page, who, role).locator('[data-action="revoke-grant"]').click();
  await confirm(page);
}

/** Narrows the grants table to those the text finds, since a busy installation's grants run to more than one page. */
export async function findGrants(page: Page, text: string): Promise<void> {
  await page.locator("[data-assignment-search]").fill(text);
}
