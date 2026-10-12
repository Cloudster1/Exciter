import { request, type APIRequestContext, type BrowserContext } from "@playwright/test";
import createClient, { type Client } from "openapi-fetch";
import type { paths } from "../../web/src/api/schema";
import { WEB_URL } from "./stack";

/** The API client the web app uses, typed from api/openapi.json, carrying a stored session. */
export type ExciterApi = Client<paths> & { dispose: () => Promise<void> };

const NO_BODY_STATUSES = new Set([101, 204, 205, 304]);

// Requests go through Playwright's own request context rather than Node's fetch, so the cookies of a
// storageState file ride along exactly as they would in the browser that saved them.
async function viaPlaywright(context: APIRequestContext, req: Request): Promise<Response> {
  const body = req.body ? Buffer.from(await req.arrayBuffer()) : undefined;
  const res = await context.fetch(req.url, {
    method: req.method,
    headers: Object.fromEntries(req.headers),
    data: body,
    maxRedirects: 0,
    failOnStatusCode: false,
  });
  return new Response(NO_BODY_STATUSES.has(res.status()) ? null : new Uint8Array(await res.body()), { status: res.status(), headers: res.headers() });
}

/** A signed-in browser state: a file the setup wrote, or one held in memory. */
export type Session = string | Awaited<ReturnType<BrowserContext["storageState"]>>;

/** A client for /api/v1 as whoever `storageState` signed in; dispose it when done. */
export async function createExciterApi(storageState?: Session): Promise<ExciterApi> {
  // The api refuses a cookie-carried write from a foreign origin, so the
  // requests say they come from the web client, as the browser's do.
  const context = await request.newContext({ baseURL: WEB_URL, storageState, extraHTTPHeaders: { Origin: WEB_URL } });
  const client = createClient<paths>({ baseUrl: `${WEB_URL}/api/v1`, fetch: (req) => viaPlaywright(context, req) });
  return Object.assign(client, { dispose: () => context.dispose() });
}

/** The data of an API answer, or an error naming the status and the api's message. */
export function must<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.error !== undefined || result.data === undefined) {
    const detail = result.error ? JSON.stringify(result.error) : "no body";
    throw new Error(`${result.response.url} answered ${result.response.status}: ${detail}`);
  }
  return result.data;
}
