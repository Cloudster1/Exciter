import { fireEvent, render, waitFor, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { onTestFinished, vi } from "vitest";
import { meQueryKey, type Me } from "@/api/auth";
import { en } from "@/i18n/en";
import { LocalizedRouter } from "@/features/shell/LocalizedRouter";
import { createQueryClient } from "@/lib/session";
import { buildRouter, sendToLogin } from "@/routes";

/** The person every shell test is signed in as unless it says otherwise: an administrator of the installation. */
export const signedIn: Me = {
  user: { id: "0195f000-0000-7000-8000-00000000000a", email: "ada@exciter.test", name: "Ada Lovelace", locale: "", timezone: "" },
  installation: { name: "Demo", oauthConnections: true },
  role: "member",
  administers: true,
  token: null,
  signsWith: "password",
};

/** The real router and the real shell, started at a path, signed in as `me`; null is nobody,
 * and then the page is wherever the router sends a visitor. `ready` is what shows the page arrived. */
export async function renderAt(path: string, { me = signedIn, ready = "[data-shell], [data-login]" }: { me?: Me | null; ready?: string } = {}) {
  // A page that reads what every installation has still finds it behind it in a test that stubs nothing.
  if (!apiStubbed) stubApi({});
  const queryClient = createQueryClient((client) => sendToLogin(router, client));
  signedInAs = me;
  if (me) queryClient.setQueryData(meQueryKey, me);
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  // Awaiting the load has no timeout, where polling for the first page would on a busy machine.
  await router.load();
  render(
    <QueryClientProvider client={queryClient}>
      <LocalizedRouter router={router} />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(document.querySelector(ready)).not.toBeNull());
  // A navigation still loading at the end of a test settles, or fires the
  // router's pending timer, after jsdom has gone, and fails a later file.
  onTestFinished(() => {
    if (router.state.status !== "idle") {
      throw new Error(`The test ended while the router was still loading ${router.state.location.pathname}. Await arrival(router, path) before it ends.`);
    }
  });
  return router;
}

/** Resolves once the router has finished loading `pathname`, lazy chunks and loaders included. */
export function arrival(router: ReturnType<typeof buildRouter>, pathname: string) {
  return new Promise<void>((resolve) => {
    const arrived = () => router.state.status === "idle" && router.state.resolvedLocation?.pathname === pathname;
    if (arrived()) return resolve();
    const stop = router.subscribe("onResolved", () => {
      if (!arrived()) return;
      stop();
      resolve();
    });
  });
}

/** One stubbed API answer: a status and a JSON body, for a method and a path under /api/v1. */
export type Answer = { status: number; body?: unknown };

/** What a new installation answers to the reads every page of the shell may make, unless a test says otherwise. */
const freshInstallation: Record<string, Answer> = {
  "GET /projects": { status: 200, body: { projects: [] } },
  "GET /notifications/unread-count": { status: 200, body: { unread: 0 } },
  "GET /reviews/waiting": { status: 200, body: { reviews: [], total: 0, limit: 20, offset: 0 } },
  "GET /oauth/clients": { status: 200, body: { clients: [] } },
  "GET /meta": { status: 200, body: { product: "exciter", apiVersion: "1", capabilities: [], build: { version: "0.0.0-test", commit: "0123456789abcdef" } } },
};

let apiStubbed = false;

// Who renderAt signed in, so what they may do across the installation follows from it unless a test says otherwise.
let signedInAs: Me | null = null;

function myAccess(): Answer {
  const administers = signedInAs?.administers ?? false;
  return { status: 200, body: { can: { administer: administers, createProject: administers } } };
}

// A file goes up as form data, which is no JSON; a test reads it as the text it is.
function parsed(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

/** Stands in for the API by "METHOD /path", anything else a 404 in its envelope,
 * and returns the requests made so a test can say what was sent. */
export function stubApi(answers: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>) {
  apiStubbed = true;
  onTestFinished(() => {
    apiStubbed = false;
  });
  const sent: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal("fetch", async (input: Request) => {
    const url = new URL(input.url, "http://app.test");
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const text = await input.clone().text();
    sent.push({ method: input.method, path, body: text ? parsed(text) : undefined });
    const key = `${input.method} ${path}`;
    const found = answers[key] ?? freshInstallation[key] ?? (key === "GET /access/me" ? myAccess() : undefined);
    const answer =
      typeof found === "function" ? await found(input) : (found ?? { status: 404, body: { error: { code: "not_found", message: "Not stubbed." } } });
    return new Response(answer.body === undefined ? null : JSON.stringify(answer.body), {
      status: answer.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  return sent;
}

/** How long a test may take that edits a whole page and checks it for accessibility, which runs past vitest's default
 * of five seconds on a busy machine. */
export const LONG_TEST_MS = 15_000;

/** A 401 in the API's envelope. */
export const unauthorized: Answer = { status: 401, body: { error: { code: "unauthorized", message: "Sign in to continue." } } };

/** Sets the viewport's width the way a browser window would change it. */
export function setViewportWidth(width: number) {
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: width });
  window.dispatchEvent(new Event("resize"));
}

/** Answers the ConfirmDialog that is open, with its danger button or with Cancel, and returns the dialog it was. */
export async function answerConfirm(yes: boolean): Promise<HTMLElement> {
  const dialog = await waitFor(() => {
    const open = document.querySelector<HTMLElement>("[data-confirm]");
    expect(open).not.toBeNull();
    return open!;
  });
  const button = yes ? dialog.querySelector<HTMLElement>('[data-action="confirm"]')! : within(dialog).getByRole("button", { name: en.kit.cancel });
  fireEvent.click(button);
  await waitFor(() => expect(document.querySelector("[data-confirm]")).toBeNull());
  return dialog;
}
