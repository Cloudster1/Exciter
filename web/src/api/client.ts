import createClient, { type Middleware } from "openapi-fetch";
import { API_BASE, HTTP_UNAUTHORIZED } from "@/config";
import { t } from "@/i18n";
import type { paths } from "./schema";

// The one place that talks to the API, typed from the server's own OpenAPI
// document: a path, a body or a response that does not exist fails to compile.

/** The error envelope every endpoint answers with when it refuses. */
export interface ApiErrorBody {
  code: string;
  message: string;
  fields?: Record<string, string>;
  /** The character a query went wrong at, counted from 1, with bad_query. */
  position?: number;
  requestId?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: Record<string, string>;
  readonly position?: number;
  readonly requestId?: string;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code;
    this.fields = body.fields ?? {};
    this.position = body.position;
    this.requestId = body.requestId;
  }

  /** True when the caller is not signed in, which the router treats specially. */
  get isUnauthenticated(): boolean {
    return this.status === HTTP_UNAUTHORIZED;
  }
}

// A non-JSON body from a proxy or gateway is still a failure that has to be
// reported usefully rather than crashing on, so it becomes a generic envelope.
const raiseOnError: Middleware = {
  async onResponse({ response }) {
    if (response.ok) return response;
    let body: { error?: ApiErrorBody } | undefined;
    try {
      body = (await response.clone().json()) as { error?: ApiErrorBody };
    } catch {
      body = undefined;
    }
    throw new ApiError(response.status, body?.error ?? { code: "unexpected_response", message: t.api.unexpected(response.status) });
  },
};

// Absolute, because a Request made outside a browser has no page to resolve a
// bare path against.
function defaultBase(): string {
  return typeof window === "undefined" ? API_BASE : new URL(API_BASE, window.location.origin).toString();
}

export function createApi(baseUrl: string = defaultBase()) {
  // The session lives in an HttpOnly cookie, so it has to be sent explicitly. fetch is looked up
  // per request, so the shared client uses whatever the page has at the time, a test's stand-in included.
  const client = createClient<paths>({ baseUrl, credentials: "same-origin", fetch: (request) => globalThis.fetch(request) });
  client.use(raiseOnError);
  return client;
}

/** The one client the application shares. */
export const api = createApi();
