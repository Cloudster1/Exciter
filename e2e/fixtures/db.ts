import type { TestInfo } from "@playwright/test";
import pg from "pg";
import { superuserURL } from "./stack";

// Straight into the stack's database, for what no endpoint makes yet. Keep it
// to arranging and tidying up; what a spec checks goes through the app.

/** Runs fn on a connection as the database's superuser, closed afterwards, once the replicas have what it wrote. */
export async function withDatabase<T>(fn: (client: pg.Client) => Promise<T>): Promise<T> {
  const client = new pg.Client({ connectionString: superuserURL() });
  await client.connect();
  try {
    const result = await fn(client);
    await settle(client);
    return result;
  } finally {
    await client.end();
  }
}

const SETTLE_MS = 10_000;
const SETTLE_RECHECK_MS = 50;

// As the integration suite's settle: wait until every replica has replayed all
// written so far, a flushed message first sending a commit still in the WAL buffers.
async function settle(client: pg.Client): Promise<void> {
  const { rows } = await client.query<{ lsn: string }>("SELECT pg_logical_emit_message(false, 'exciter-settle', '', true)::text AS lsn");
  const lsn = rows[0]!.lsn;
  const deadline = Date.now() + SETTLE_MS;
  for (;;) {
    const behind = await client.query("SELECT 1 FROM pg_stat_replication WHERE replay_lsn IS NULL OR replay_lsn < $1::pg_lsn", [lsn]);
    if (behind.rowCount === 0) return;
    if (Date.now() > deadline) throw new Error(`A replica has not replayed ${lsn} after ${SETTLE_MS} ms. Is it streaming from the primary?`);
    await new Promise((resolve) => setTimeout(resolve, SETTLE_RECHECK_MS));
  }
}

/** Nothing listens on port 9 of the api's own container, so asking this issuer who it is fails at once. */
export const DEAD_ISSUER = "http://127.0.0.1:9/realms/nowhere";

/** An organization of its own whose provider cannot be reached; call remove when done. */
export async function orgWithDeadProvider(testInfo: TestInfo): Promise<{ slug: string; remove: () => Promise<void> }> {
  const slug = `e2e-dead-${testInfo.workerIndex}-${Date.now().toString(36)}`;
  await withDatabase(async (db) => {
    const { rows } = await db.query<{ id: string }>("INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id", [slug, "Dead provider"]);
    await db.query("INSERT INTO oidc_provider (org_id, issuer, client_id) VALUES ($1, $2, 'exciter')", [rows[0]!.id, DEAD_ISSUER]);
  });
  return {
    slug,
    remove: () => withDatabase(async (db) => void (await db.query("DELETE FROM org WHERE slug = $1", [slug]))),
  };
}
