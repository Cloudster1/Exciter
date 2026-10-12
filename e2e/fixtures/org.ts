import { basename } from "node:path";
import { request, type Browser } from "@playwright/test";
import type { Session } from "./api";
import { test, signInToOrg, USERS, type User } from "./auth";
import { testEndpointsToken, WEB_URL } from "./stack";

/** An organization made for one spec file alone, with alice (admin) and bob (member) signed in to it. */
export interface FreshOrg {
  slug: string;
  id: string;
  sessions: Record<User, Exclude<Session, string>>;
}

/** The api's test endpoints, which the compose stack switches on and no deployment does. */
const TEST_ORGS_PATH = "/api/v1/test/orgs";
const TEST_TOKEN_HEADER = "X-Exciter-Test-Token";

async function testEndpoints() {
  return request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { [TEST_TOKEN_HEADER]: testEndpointsToken() } });
}

async function createOrg(label: string): Promise<{ slug: string; id: string }> {
  const context = await testEndpoints();
  try {
    const res = await context.post(TEST_ORGS_PATH, { data: { label } });
    if (res.status() !== 201) {
      throw new Error(`POST ${TEST_ORGS_PATH} answered ${res.status()}: ${await res.text()}. Is the stack up with EXCITER_TEST_ENDPOINTS on?`);
    }
    return (await res.json()) as { slug: string; id: string };
  } finally {
    await context.dispose();
  }
}

async function deleteOrg(slug: string): Promise<void> {
  const context = await testEndpoints();
  try {
    const res = await context.delete(`${TEST_ORGS_PATH}/${encodeURIComponent(slug)}`);
    if (res.status() !== 204) throw new Error(`DELETE ${TEST_ORGS_PATH}/${slug} answered ${res.status()}: ${await res.text()}`);
  } finally {
    await context.dispose();
  }
}

async function makeFreshOrg(browser: Browser, label: string): Promise<FreshOrg> {
  const org = await createOrg(label);
  const users = Object.keys(USERS) as User[];
  const signedIn = await Promise.all(users.map((user) => signInToOrg(browser, user, org.slug))).catch(async (error: unknown) => {
    await deleteOrg(org.slug).catch(() => {});
    throw error;
  });
  return { ...org, sessions: Object.fromEntries(users.map((user, i) => [user, signedIn[i]!])) as FreshOrg["sessions"] };
}

/** "themes.spec.ts" becomes "themes", so a leftover organization names the spec that made it. */
function labelOf(file: string): string {
  return basename(file).replace(/\.spec\.ts$/, "");
}

interface OrgPool {
  /** The organization of one spec file in this worker, made on first use. */
  forFile: (file: string) => Promise<FreshOrg>;
}

/** The test for a spec that owns its organization: alice and bob act in one made for the spec file, which the
 *  worker removes with all its data when it stops. Tests of one file in the same worker share it, one after another. */
export const orgTest = test.extend<{ freshOrg: FreshOrg }, { orgPool: OrgPool }>({
  orgPool: [
    async ({ browser }, use) => {
      const made = new Map<string, Promise<FreshOrg>>();
      await use({
        forFile: (file) => {
          let org = made.get(file);
          if (!org) {
            org = makeFreshOrg(browser, labelOf(file));
            made.set(file, org);
          }
          return org;
        },
      });
      const failures: unknown[] = [];
      for (const pending of made.values()) {
        const org = await pending.catch(() => undefined);
        if (org) await deleteOrg(org.slug).catch((error: unknown) => failures.push(error));
      }
      if (failures.length > 0) throw new AggregateError(failures, "Some throwaway organizations could not be removed.");
    },
    { scope: "worker" },
  ],
  freshOrg: async ({ orgPool }, use, testInfo) => {
    await use(await orgPool.forFile(testInfo.file));
  },
  sessionOf: async ({ freshOrg }, use) => {
    await use((user) => freshOrg.sessions[user]);
  },
});
