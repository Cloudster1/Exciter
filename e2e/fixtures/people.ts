import { randomBytes } from "node:crypto";
import { basename } from "node:path";
import { expect, request } from "@playwright/test";
import { createExciterApi, must, type ExciterApi, type Session } from "./api";
import { ownerApi, test, type User } from "./auth";
import { WEB_URL } from "./stack";

/** Somebody made in the installation for one spec file alone, signed in with a password. */
export interface Person {
  id: string;
  email: string;
  name: string;
  session: Exclude<Session, string>;
}

/** Each spec's people stand where alice and bob stand in demo. */
const ROLES: Record<User, "admin" | "member"> = { alice: "admin", bob: "member" };
const FIRST_NAMES: Record<User, string> = { alice: "Alice", bob: "Bob" };
const PASSWORD_BYTES = 18;
const LOGIN_PATH = "/api/v1/auth/login";

/** "themes.spec.ts" becomes "themes", so a leftover person names the spec that made them. */
function labelOf(file: string): string {
  return basename(file).replace(/\.spec\.ts$/, "");
}

// A password sign-in may read the account from a replica that has not caught up with the owner making it.
async function signInAs(email: string, password: string): Promise<Exclude<Session, string>> {
  const context = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Origin: WEB_URL } });
  try {
    await expect.poll(async () => (await context.post(LOGIN_PATH, { data: { email, password } })).status()).toBe(200);
    return await context.storageState();
  } finally {
    await context.dispose();
  }
}

async function makePerson(owner: ExciterApi, user: User, label: string, workerIndex: number): Promise<Person> {
  const tag = `${label}-${workerIndex}-${Date.now().toString(36)}`;
  const email = `${user}-${tag}@e2e.exciter.test`;
  const name = `${FIRST_NAMES[user]} ${tag}`;
  const password = randomBytes(PASSWORD_BYTES).toString("base64url");
  const made = must(await owner.POST("/users", { body: { email, name, role: ROLES[user], password } })).member;
  const session = await signInAs(email, password);
  // The built-in theme over any default, so a default the themes spec sets meanwhile never reaches this spec's axe.
  const api = await createExciterApi(session);
  try {
    must(await api.PUT("/themes/active", { body: { themeId: null, builtIn: true } }));
  } finally {
    await api.dispose();
  }
  return { id: made.userId, email, name, session };
}

interface PeoplePool {
  /** The people of one spec file in this worker, made on first use. */
  forFile: (file: string) => Promise<Record<User, Person>>;
}

/** The test for a spec that keeps to its own people: alice, an administrator, and bob, a member, are made in the
 *  installation for the spec file and let go when the worker stops; the file's projects carry keys of their own. */
export const peopleTest = test.extend<{ people: Record<User, Person> }, { peoplePool: PeoplePool }>({
  peoplePool: [
    async ({}, use, workerInfo) => {
      const owner = await ownerApi();
      const made = new Map<string, Promise<Record<User, Person>>>();
      await use({
        forFile: (file) => {
          let people = made.get(file);
          if (!people) {
            const label = labelOf(file);
            people = Promise.all([makePerson(owner, "alice", label, workerInfo.workerIndex), makePerson(owner, "bob", label, workerInfo.workerIndex)]).then(
              ([alice, bob]) => ({ alice, bob }),
            );
            made.set(file, people);
          }
          return people;
        },
      });
      const failures: unknown[] = [];
      for (const pending of made.values()) {
        const people = await pending.catch(() => undefined);
        for (const person of people ? Object.values(people) : []) {
          const gone = await owner.DELETE("/users/{userID}", { params: { path: { userID: person.id } } });
          if (gone.response.status !== 204) failures.push(new Error(`${person.email} could not be let go: ${gone.response.status}`));
        }
      }
      await owner.dispose();
      if (failures.length > 0) throw new AggregateError(failures, "Some of the specs' people could not be let go.");
    },
    { scope: "worker" },
  ],
  people: async ({ peoplePool }, use, testInfo) => {
    await use(await peoplePool.forFile(testInfo.file));
  },
  sessionOf: async ({ people }, use) => {
    await use((user) => people[user].session);
  },
});
