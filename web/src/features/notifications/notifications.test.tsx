import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Thread } from "@/api/comments";
import { NOTIFICATION_KINDS, type Notification, type Preferences } from "@/api/notifications";
import { UNREAD_POLL_MS } from "@/config";
import { notificationsDe } from "@/i18n/notifications.de";
import { notificationsEn } from "@/i18n/notifications.en";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const requirementId = "0195f000-0000-7000-8000-0000000000a1";
const threadId = "0195f000-0000-7000-8000-00000000c0a1";

function aNotification(over: Partial<Notification> = {}): Notification {
  return {
    id: "0195f000-0000-7000-8000-0000000000e1",
    kind: "revised",
    actorName: "Bob Builder",
    requirement: {
      id: requirementId,
      key: "CHK-R1",
      projectKey: "CHK",
      kind: "requirement",
      title: "Pay the basket",
      revision: 3,
      status: null,
      trashed: false,
    },
    revision: 3,
    threadId: null,
    commentId: null,
    reviewId: null,
    linkId: null,
    excerpt: "Tightened the limit.",
    createdAt: "2026-10-01T08:00:00Z",
    readAt: null,
    session: null,
    codeScan: null,
    execution: null,
    ...over,
  };
}

const page = (notifications: Notification[]) => ({ status: 200, body: { notifications, total: notifications.length, limit: 20, offset: 0 } });

function stubShell(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/comments": { status: 200, body: { threads: [] } },
    ...more,
  });
}

const unreadAsked = (sent: { method: string; path: string }[]) => sent.filter((each) => each.path === "/notifications/unread-count").length;

describe("the notification bell", () => {
  it("shows the unread count as a badge and says it in its name", async () => {
    stubShell({ "GET /notifications/unread-count": { status: 200, body: { unread: 3 } } });
    await renderAt("/projects");
    const bell = await screen.findByRole("button", { name: "Notifications, 3 unread" });
    expect(bell).toHaveAttribute("aria-haspopup", "dialog");
    expect(document.querySelector("[data-unread-badge]")).toHaveTextContent("3");
  });

  it("shows no badge while nothing is unread", async () => {
    stubShell();
    await renderAt("/projects");
    expect(await screen.findByRole("button", { name: "Notifications" })).toBeInTheDocument();
    expect(document.querySelector("[data-unread-badge]")).toBeNull();
  });

  it("asks again when the window regains focus and every poll interval", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let unread = 1;
    const sent = stubShell({ "GET /notifications/unread-count": () => ({ status: 200, body: { unread } }) });
    await renderAt("/projects");
    await screen.findByRole("button", { name: "Notifications, 1 unread" });
    const before = unreadAsked(sent);
    unread = 2;
    window.dispatchEvent(new Event("focus"));
    await screen.findByRole("button", { name: "Notifications, 2 unread" });
    expect(unreadAsked(sent)).toBeGreaterThan(before);
    unread = 5;
    await vi.advanceTimersByTimeAsync(UNREAD_POLL_MS);
    await screen.findByRole("button", { name: "Notifications, 5 unread" });
  });

  it("lists what happened in the reader's words, and following one marks it read and opens the requirement", async () => {
    let read = false;
    const sent = stubShell({
      "GET /notifications/unread-count": () => ({ status: 200, body: { unread: read ? 0 : 1 } }),
      "GET /notifications": () =>
        page([
          aNotification({ readAt: read ? "2026-10-01T09:00:00Z" : null }),
          aNotification({
            id: "0195f000-0000-7000-8000-0000000000e2",
            kind: "transitioned",
            actorName: "",
            excerpt: "Approved",
            readAt: "2026-10-01T07:00:00Z",
          }),
        ]),
      "POST /notifications/read": () => {
        read = true;
        return { status: 204 };
      },
    });
    const router = await renderAt("/projects");
    await userEvent.click(await screen.findByRole("button", { name: "Notifications, 1 unread" }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    const [first, second] = within(panel).getAllByRole("link", { name: /CHK-R1/ }) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Unread: Bob Builder saved a new revision of CHK-R1");
    expect(first).toHaveTextContent("Pay the basket");
    expect(first).toHaveTextContent("Tightened the limit.");
    expect(second).toHaveTextContent("Somebody moved CHK-R1 to another status");
    expect(second).not.toHaveTextContent("Unread");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(first);
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CHK/r/CHK-R1"));
    expect(sent.find((each) => each.method === "POST" && each.path === "/notifications/read")?.body).toEqual({ ids: ["0195f000-0000-7000-8000-0000000000e1"] });
    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
    await screen.findByRole("button", { name: "Notifications" });
  });

  it("words every kind", async () => {
    const kinds: Notification["kind"][] = ["mentioned", "review_requested", "reviewed", "suspect", "replied", "commented", "resolved"];
    stubShell({
      "GET /notifications/unread-count": { status: 200, body: { unread: kinds.length } },
      "GET /notifications": page(kinds.map((kind, i) => aNotification({ id: `0195f000-0000-7000-8000-0000000001${i}0`, kind }))),
    });
    await renderAt("/projects");
    await userEvent.click(await screen.findByRole("button", { name: `Notifications, ${kinds.length} unread` }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    for (const words of [
      "Bob Builder mentioned you in a comment on CHK-R1",
      "Bob Builder asks you to review CHK-R1",
      "Bob Builder decided the review of CHK-R1",
      "A link of CHK-R1 needs checking after Bob Builder revised its other end",
      "Bob Builder replied in a thread on CHK-R1",
      "Bob Builder commented on CHK-R1",
      "Bob Builder resolved or reopened a thread on CHK-R1",
    ]) {
      expect(within(panel).getByText(words)).toBeInTheDocument();
    }
  });

  it("leads a reply to its thread, which the requirement brings into view", async () => {
    const thread: Thread = {
      id: threadId,
      requirementId,
      resolved: false,
      resolvedByName: "",
      resolvedAt: null,
      can: { reply: true, resolve: true },
      comments: [
        {
          id: threadId,
          via: null,
          threadId,
          authorId: "0195f000-0000-7000-8000-0000000000e9",
          authorName: "Bob Builder",
          revision: 3,
          body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "Why?" }] }] },
          deleted: false,
          createdAt: "2026-10-01T08:00:00Z",
          editedAt: null,
          can: { edit: false, delete: false },
        },
      ],
    };
    stubShell({
      "GET /notifications/unread-count": { status: 200, body: { unread: 1 } },
      "GET /notifications": page([aNotification({ kind: "replied", threadId, commentId: threadId, excerpt: "Why?" })]),
      "POST /notifications/read": { status: 204 },
      "GET /requirements/CHK-R1/comments": { status: 200, body: { threads: [thread] } },
      "GET /requirements/CHK-R1/mentionable": { status: 200, body: { people: [] } },
    });
    Element.prototype.scrollIntoView = vi.fn();
    const router = await renderAt("/projects");
    await userEvent.click(await screen.findByRole("button", { name: "Notifications, 1 unread" }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    await userEvent.click(within(panel).getByRole("link", { name: /replied in a thread/ }));
    await waitFor(() => expect(router.state.location.hash).toBe(`thread-${threadId}`));
    await waitFor(() => expect(document.querySelector(`[data-thread="${threadId}"]`)).toHaveFocus());
  });

  it("marks everything read at once, and closes on Escape with focus back on the bell", async () => {
    const sent = stubShell({
      "GET /notifications/unread-count": { status: 200, body: { unread: 1 } },
      "GET /notifications": page([aNotification()]),
      "POST /notifications/read": { status: 204 },
    });
    await renderAt("/projects");
    const bell = await screen.findByRole("button", { name: "Notifications, 1 unread" });
    bell.focus();
    await userEvent.keyboard("{Enter}");
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    await userEvent.click(within(panel).getByRole("button", { name: "Mark all as read" }));
    await waitFor(() => expect(sent.find((each) => each.path === "/notifications/read")?.body).toEqual({ all: true }));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
    expect(bell).toHaveFocus();
  });

  it("says so when there is nothing, and links to everything and to the settings", async () => {
    stubShell({ "GET /notifications": page([]) });
    await renderAt("/projects");
    await userEvent.click(await screen.findByRole("button", { name: "Notifications" }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    expect(within(panel).getByText(/Nothing new/)).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: "Mark all as read" })).toBeDisabled();
    expect(within(panel).getByRole("link", { name: "Notification settings" })).toHaveAttribute("href", "/settings/notifications");
    expect(within(panel).getByRole("link", { name: "See all notifications" })).toHaveAttribute("href", "/notifications");
  });
});

describe("the notifications page", () => {
  it("lists everything, or the unread alone", async () => {
    const sent = stubShell({
      "GET /notifications": (request) => {
        const unread = new URL(request.url).searchParams.get("unread") === "true";
        return page(unread ? [] : [aNotification({ readAt: "2026-10-01T09:00:00Z" })]);
      },
    });
    await renderAt("/notifications");
    const list = await screen.findByRole("list", { name: "Notifications" });
    expect(within(list).getByRole("link", { name: /saved a new revision of CHK-R1/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("checkbox", { name: "Unread only" }));
    expect(await screen.findByText("You have read everything.")).toBeInTheDocument();
    expect(sent.some((each) => each.path === "/notifications" && each.method === "GET")).toBe(true);
  });
});

const allOn = {
  mentioned: true,
  review_requested: true,
  reviewed: true,
  suspect: true,
  replied: true,
  commented: true,
  resolved: true,
  revised: true,
  transitioned: true,
};
const defaults: Preferences = { inApp: { ...allOn }, email: { ...allOn }, digest: "off", autoWatch: true };

describe("the notification settings", () => {
  it("switch kinds in the app and by email, choose the digest and auto watch, and save the whole", async () => {
    const sent = stubShell({
      "GET /notification-preferences": { status: 200, body: { preferences: defaults } },
      "PUT /notification-preferences": async (request) => ({ status: 200, body: { preferences: await request.json() } }),
    });
    await renderAt("/settings/notifications");
    const inApp = await screen.findByRole("checkbox", { name: "In the app: A requirement you watch gets a new revision" });
    const email = screen.getByRole("checkbox", { name: "By email: A requirement you watch gets a new revision" });
    expect(inApp).toBeChecked();
    expect(email).toBeChecked();
    await userEvent.click(inApp);
    expect(email).toBeDisabled();
    expect(email).not.toBeChecked();
    await userEvent.click(screen.getByRole("checkbox", { name: "By email: Somebody mentions you in a comment" }));
    await userEvent.selectOptions(screen.getByLabelText("When emails go out"), "daily");
    expect(screen.getByRole("option", { name: "One email a day at 08:00 UTC with everything new" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: "Watch the requirements I write, revise or am made the owner of" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Your notification settings are saved.")).toBeInTheDocument();
    expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
      inApp: { ...allOn, revised: false },
      email: { ...allOn, mentioned: false },
      digest: "daily",
      autoWatch: false,
    });
  });

  it("offer every kind the API tells, in its order, worded in both languages", async () => {
    const openapi = JSON.parse(readFileSync(resolve(process.cwd(), "../api/openapi.json"), "utf8")) as {
      components: { schemas: { Notification: { properties: { kind: { enum: string[] } } } } };
    };
    const told = openapi.components.schemas.Notification.properties.kind.enum;
    expect(NOTIFICATION_KINDS).toEqual(told);
    for (const kind of NOTIFICATION_KINDS) {
      expect(notificationsEn.kinds[kind], `the English words for ${kind}`).toBeTruthy();
      expect(notificationsDe.kinds[kind], `the German words for ${kind}`).toBeTruthy();
    }
    stubShell({ "GET /notification-preferences": { status: 200, body: { preferences: defaults } } });
    await renderAt("/settings/notifications");
    expect(await screen.findByRole("checkbox", { name: "In the app: Somebody asks you to take part in a review session" })).toBeChecked();
    expect(document.querySelectorAll("[data-preference]")).toHaveLength(told.length);
  });

  it("read a kind the stored switches leave out as on", async () => {
    stubShell({ "GET /notification-preferences": { status: 200, body: { preferences: { ...defaults, inApp: {}, email: { revised: false } } } } });
    await renderAt("/settings/notifications");
    expect(await screen.findByRole("checkbox", { name: "In the app: A requirement you watch gets a new revision" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "By email: A requirement you watch gets a new revision" })).not.toBeChecked();
  });

  it("are reached from the account menu", async () => {
    stubShell({ "GET /notification-preferences": { status: 200, body: { preferences: defaults } } });
    const router = await renderAt("/projects");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Notification settings" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/notifications"));
  });
});
