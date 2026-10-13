import pg from "pg";
import { ORG_SLUG } from "./auth";
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

const PROVIDER_ISSUER = "SELECT p.issuer FROM oidc_provider p JOIN org o ON o.id = p.org_id WHERE o.slug = $1";
const SET_ISSUER = "UPDATE oidc_provider SET issuer = $1 WHERE org_id = (SELECT id FROM org WHERE slug = $2)";

/** Points demo's provider at an issuer nobody answers for, until restore puts the one it had back. */
export async function deadProvider(): Promise<{ restore: () => Promise<void> }> {
  const issuer = await withDatabase(async (db) => {
    const { rows } = await db.query<{ issuer: string }>(PROVIDER_ISSUER, [ORG_SLUG]);
    if (!rows[0]) throw new Error(`${ORG_SLUG} has no identity provider to make unreachable. Did the seed configure one?`);
    await db.query(SET_ISSUER, [DEAD_ISSUER, ORG_SLUG]);
    return rows[0].issuer;
  });
  return { restore: () => withDatabase(async (db) => void (await db.query(SET_ISSUER, [issuer, ORG_SLUG]))) };
}
