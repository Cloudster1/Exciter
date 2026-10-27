import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NOTIFICATION_KINDS, type Notification, type Preferences } from "@/api/notifications";
import { NOTIFICATIONS_PAGE_SIZE } from "@/config";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";
import { t } from "@/i18n";

// The edges of notifications (#312): where a row about a session, an execution or a code scan leads, a row about
// nothing, following one already read, the notifications page a page at a time, and a switch for every kind.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const ROWS_IN_ALL = 120;

function aNotification(over: Partial<Notification> = {}): Notification {
  return {
    id: "0195f000-0000-7000-8000-0000000000e1",
    kind: "revised",
    actorName: "Bob Builder",
    requirement: null,
    revision: 0,
    threadId: null,
    commentId: null,
    reviewId: null,
    linkId: null,
    excerpt: "",
    createdAt: "2026-10-01T08:00:00Z",
    readAt: null,
    session: null,
    codeScan: null,
    execution: null,
    ...over,
  };
}

const page = (notifications: Notification[], total = notifications.length, offset = 0) => ({
  status: 200,
  body: { notifications, total, limit: NOTIFICATIONS_PAGE_SIZE, offset },
});

function stubShell(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>) {
  return stubApi({ "GET /notifications/unread-count": { status: 200, body: { unread: 1 } }, ...more });
}

async function openBell() {
  await userEvent.click(await screen.findByRole("button", { name: /^Notifications/ }));
  return screen.findByRole("dialog", { name: "Notifications" });
}

describe("a notification row", () => {
  it.each([
    [
      "a review session's",
      aNotification({ kind: "session_invited", session: { id: "0195f000-0000-7000-8000-0000000005e1", title: "Brakes review", projectKey: "CHK" } }),
      "/p/CHK/sessions/0195f000-0000-7000-8000-0000000005e1",
    ],
    [
      "a test execution's",
      aNotification({ kind: "execution_assigned", execution: { id: "0195f000-0000-7000-8000-0000000006e1", title: "Night run", projectKey: "CHK" } }),
      "/p/CHK/tests/executions/0195f000-0000-7000-8000-0000000006e1",
    ],
    [
      "a code scan's",
      aNotification({
        kind: "code_outdated",
        codeScan: { id: "0195f000-0000-7000-8000-0000000007e1", repository: "brakes", branch: "main", projectKey: "CHK" },
      }),
      "/p/CHK/code-tags",
    ],
  ])("leads %s to its page", async (_, notification, href) => {
    stubShell({ "GET /notifications": page([notification]) });
    await renderAt("/projects");
    const panel = await openBell();
    expect(within(panel).getByRole("link", { name: /Unread/ })).toHaveAttribute("href", href);
  });

  it("is words alone when it is about nothing it can lead to", async () => {
    stubShell({ "GET /notifications": page([aNotification({ kind: "commented" })]) });
    await renderAt("/projects");
    const panel = await openBell();
    const row = panel.querySelector('[data-notification="commented"]') as HTMLElement;
    expect(row.tagName).toBe("DIV");
    expect(within(panel).queryByRole("link", { name: /Unread/ })).toBeNull();
  });

  it("marks nothing read when the one followed was read already", async () => {
    const sent = stubShell({
      "GET /notifications": page([
        aNotification({
          readAt: "2026-10-01T09:00:00Z",
          execution: { id: "0195f000-0000-7000-8000-0000000006e1", title: "Night run", projectKey: "CHK" },
          kind: "execution_closed",
        }),
      ]),
      "GET /projects/CHK": { status: 404, body: { error: { code: "not_found", message: "Not here." } } },
    });
    const router = await renderAt("/projects");
    const panel = await openBell();
    await userEvent.click(within(panel).getByRole("link", { name: /Night run/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/CHK/tests/executions/0195f000-0000-7000-8000-0000000006e1"));
    await arrival(router, "/p/CHK/tests/executions/0195f000-0000-7000-8000-0000000006e1");
    expect(sent.some((each) => each.method === "POST" && each.path === "/notifications/read")).toBe(false);
  });
});

describe("the notifications page", () => {
  it("walks the pages when there are more than one, and goes back to the first for the unread", async () => {
    const asked: { offset: number; unread: boolean }[] = [];
    stubShell({
      "GET /notifications": (request) => {
        const query = new URL(request.url).searchParams;
        const offset = Number(query.get("offset") ?? 0);
        const unread = query.get("unread") === "true";
        asked.push({ offset, unread });
        const shown = Math.min(NOTIFICATIONS_PAGE_SIZE, ROWS_IN_ALL - offset);
        const rows = Array.from({ length: shown }, (_, i) =>
          aNotification({ id: `0195f000-0000-7000-8000-${String(offset + i).padStart(12, "0")}`, kind: "commented", readAt: "2026-10-01T09:00:00Z" }),
        );
        return page(rows, ROWS_IN_ALL, offset);
      },
    });
    await renderAt("/notifications");
    expect(await screen.findByText(t.notifications.range(1, NOTIFICATIONS_PAGE_SIZE, ROWS_IN_ALL))).toBeInTheDocument();
    expect(screen.getByRole("button", { name: t.notifications.previous })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: t.notifications.next }));
    expect(await screen.findByText("51 to 100 of 120")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: t.notifications.next }));
    expect(await screen.findByText("101 to 120 of 120")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: t.notifications.next })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: t.notifications.previous }));
    expect(await screen.findByText("51 to 100 of 120")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: "Unread only" }));
    await waitFor(() => expect(asked.at(-1)).toEqual({ offset: 0, unread: true }));
    expect(await screen.findByText("1 to 50 of 120")).toBeInTheDocument();
  });
});

describe("the notification settings for every kind", () => {
  it("offer a switch in the app and by email for each, and save one switched off as false", async () => {
    const allOn = Object.fromEntries(NOTIFICATION_KINDS.map((kind) => [kind, true]));
    const defaults: Preferences = { inApp: { ...allOn }, email: { ...allOn }, digest: "off", autoWatch: true };
    const sent = stubApi({
      "GET /notification-preferences": { status: 200, body: { preferences: defaults } },
      "PUT /notification-preferences": async (request) => ({ status: 200, body: { preferences: await request.json() } }),
    });
    await renderAt("/settings/notifications");
    for (const kind of NOTIFICATION_KINDS) {
      const words = t.notifications.kinds[kind];
      expect(await screen.findByRole("checkbox", { name: t.notifications.inAppFor(words) }), kind).toBeChecked();
      expect(screen.getByRole("checkbox", { name: t.notifications.emailFor(words) }), kind).toBeChecked();
    }
    const last = NOTIFICATION_KINDS.at(-1)!;
    await userEvent.click(screen.getByRole("checkbox", { name: t.notifications.emailFor(t.notifications.kinds[last]) }));
    await userEvent.click(screen.getByRole("checkbox", { name: t.notifications.inAppFor(t.notifications.kinds.code_orphaned) }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Your notification settings are saved.");
    const saved = sent.find((each) => each.method === "PUT")?.body as Preferences;
    expect(saved.email).toEqual({ ...allOn, [last]: false });
    expect(saved.inApp).toEqual({ ...allOn, code_orphaned: false });
  });
});
