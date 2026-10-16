import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Webhook, WebhookDelivery } from "@/api/webhooks";
import { WEBHOOK_SIGNATURE_HEADER, WEBHOOK_TOPICS, WEBHOOKS_PATH } from "@/config";
import { en } from "@/i18n/en";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { deliveryDetail, topicName, versionName } from "./Webhooks";

afterEach(() => vi.unstubAllGlobals());

const hookId = "0199a000-0000-7000-8000-0000000000e1";

const hook = (over: Partial<Webhook> = {}): Webhook => ({
  id: hookId,
  name: "Team chat",
  url: "https://hooks.example.com/exciter",
  topics: ["requirement.revised", "comment.created"],
  projects: [{ id: "0199a000-0000-7000-8000-0000000000f1", key: "CHK", name: "Checkout" }],
  enabled: true,
  version: 1,
  disabledReason: null,
  failures: 0,
  owner: { id: signedIn.user.id, name: "Ada Lovelace" },
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
  ...over,
});

const delivery = (over: Partial<WebhookDelivery> = {}): WebhookDelivery => ({
  id: crypto.randomUUID(),
  webhookId: hookId,
  eventId: crypto.randomUUID(),
  topic: "requirement.revised",
  attempt: 1,
  manual: false,
  state: "delivered",
  status: 204,
  error: "",
  nextAttemptAt: null,
  attemptedAt: "2026-10-01T09:00:00Z",
  deliveredAt: "2026-10-01T09:00:00Z",
  createdAt: "2026-10-01T09:00:00Z",
  ...over,
});

function webhooks(list: Webhook[], extra: Record<string, Answer | ((request: Request) => Answer)> = {}) {
  return stubApi({
    "GET /webhooks": { status: 200, body: { webhooks: list } },
    "GET /projects": { status: 200, body: { projects: [aProject()] } },
    ...extra,
  });
}

describe("the webhooks page", () => {
  it("lists each webhook with its events, its state and whom it sends as", async () => {
    webhooks([
      hook(),
      hook({
        id: "0199a000-0000-7000-8000-0000000000e2",
        name: "Search index",
        enabled: false,
        disabledReason: "failing",
        failures: 6,
        owner: null,
        topics: ["*"],
        projects: [],
      }),
    ]);
    await renderAt(WEBHOOKS_PATH);
    const table = await screen.findByRole("table");
    const [, chat, index] = within(table).getAllByRole("row");
    expect(chat).toHaveTextContent("Team chat");
    expect(chat).toHaveTextContent("https://hooks.example.com/exciter");
    expect(chat).toHaveTextContent("A requirement gets a new revision, A comment is added");
    expect(chat).toHaveTextContent("CHK");
    expect(chat).toHaveTextContent("Ada Lovelace");
    expect(chat).toHaveTextContent("On");
    expect(index).toHaveTextContent("Turned off: failing");
    expect(index).toHaveTextContent("Turned off after 6 failed attempts");
    expect(index).toHaveTextContent("Everything");
    expect(index).toHaveTextContent("Every project");
    expect(index).toHaveTextContent("Nobody. Save it again");
    expect(await axeViolations()).toEqual([]);
  });

  it("adds a webhook and shows its secret once, with the header that carries the signature", async () => {
    const made = hook({ secret: "exciter_whs_once" });
    const sent = webhooks([], { "POST /webhooks": { status: 201, body: { webhook: made } } });
    await renderAt(WEBHOOKS_PATH);
    await screen.findByText("No webhooks yet");
    await userEvent.type(screen.getByLabelText("Name"), "  Team chat ");
    await userEvent.type(screen.getByLabelText(/^Address/), "https://hooks.example.com/exciter");
    await userEvent.click(screen.getByRole("checkbox", { name: "Everything, events added later too" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "A requirement gets a new revision" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "A comment is added" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "CHK Checkout" }));
    await userEvent.click(screen.getByRole("button", { name: "Add webhook" }));

    const fresh = await screen.findByLabelText("Secret for Team chat");
    expect(fresh).toHaveValue("exciter_whs_once");
    expect(screen.getByText(new RegExp(WEBHOOK_SIGNATURE_HEADER))).toBeInTheDocument();
    expect(sent.find((s) => s.method === "POST")?.body).toEqual({
      name: "Team chat",
      url: "https://hooks.example.com/exciter",
      topics: ["requirement.revised", "comment.created"],
      projects: ["CHK"],
      version: 1,
    });
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Secret for Team chat")).toBeNull();
  });

  it("lets a webhook be posted as a CloudEvent, chosen when it is added or switched later, and shows each one's envelope", async () => {
    const cloud = hook({ id: "0199a000-0000-7000-8000-0000000000e3", name: "Broker", version: 2, secret: "exciter_whs_once" });
    const sent = webhooks([hook()], {
      "POST /webhooks": { status: 201, body: { webhook: cloud } },
      "PATCH /webhooks/0199a000-0000-7000-8000-0000000000e1": { status: 200, body: { webhook: hook({ version: 2 }) } },
    });
    await renderAt(WEBHOOKS_PATH);
    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("row")[1]).toHaveTextContent("Version 1");

    const choice = screen.getByLabelText(en.webhooks.version);
    expect(choice).toHaveValue("1");
    expect(choice).toHaveAccessibleDescription(en.webhooks.versionHint);
    await userEvent.type(screen.getByLabelText("Name"), "Broker");
    await userEvent.type(screen.getByLabelText(/^Address/), "https://broker.example.com/events");
    await userEvent.selectOptions(choice, "Version 2, CloudEvents");
    await userEvent.click(screen.getByRole("button", { name: "Add webhook" }));
    await screen.findByLabelText("Secret for Broker");
    expect(sent.find((s) => s.method === "POST")?.body).toMatchObject({ name: "Broker", version: 2 });
    expect(screen.getByLabelText(en.webhooks.version)).toHaveValue("1");

    await userEvent.click(screen.getByRole("button", { name: "Actions for Team chat" }));
    expect(screen.queryByRole("menuitem", { name: "Send in Version 1" })).toBeNull();
    await userEvent.click(screen.getByRole("menuitem", { name: "Send in Version 2, CloudEvents" }));
    await waitFor(() =>
      expect(sent.find((s) => s.method === "PATCH")?.body).toEqual({
        name: "Team chat",
        url: "https://hooks.example.com/exciter",
        topics: ["requirement.revised", "comment.created"],
        projects: ["CHK"],
        version: 2,
      }),
    );
    expect(await screen.findByText("Team chat is sent in Version 2, CloudEvents from the next delivery on.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says what is missing before asking, and shows what the server refused beside its field", async () => {
    const sent = webhooks([], {
      "POST /webhooks": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { url: "Exciter may not post to an address inside the server's own network." },
          },
        },
      },
    });
    await renderAt(WEBHOOKS_PATH);
    await screen.findByText("No webhooks yet");
    await userEvent.click(screen.getByRole("checkbox", { name: "Everything, events added later too" }));
    await userEvent.click(screen.getByRole("button", { name: "Add webhook" }));
    expect(screen.getByText(en.webhooks.nameMissing)).toBeInTheDocument();
    expect(screen.getByText(en.webhooks.urlMissing)).toBeInTheDocument();
    expect(screen.getByText(en.webhooks.topicsMissing)).toBeInTheDocument();
    expect(sent.filter((s) => s.method === "POST")).toHaveLength(0);

    await userEvent.type(screen.getByLabelText("Name"), "Inside");
    await userEvent.type(screen.getByLabelText(/^Address/), "http://10.0.0.1/hook");
    await userEvent.click(screen.getByRole("checkbox", { name: "A requirement moves to another project" }));
    await userEvent.click(screen.getByRole("button", { name: "Add webhook" }));
    expect(await screen.findByText(/inside the server's own network/)).toBeInTheDocument();
    expect(screen.getByLabelText(/^Address/)).toHaveAttribute("aria-invalid", "true");
  });

  it("sends a ping, turns a webhook off and reads its delivery log, where an attempt is sent again", async () => {
    const log = [
      delivery({ topic: "ping", manual: true }),
      delivery({ state: "failed", status: 503, error: "The receiver answered 503. Check its log, then send it again.", deliveredAt: null }),
      delivery({ state: "pending", attempt: 2, status: null, attemptedAt: null, deliveredAt: null, nextAttemptAt: "2026-10-01T09:01:00Z" }),
      delivery({
        state: "withheld",
        status: null,
        error: "Nothing was sent: the webhook's owner may not view this requirement, or it no longer exists.",
        deliveredAt: null,
      }),
    ];
    const sent = webhooks([hook()], {
      "POST /webhooks/0199a000-0000-7000-8000-0000000000e1/test": { status: 200, body: { delivery: delivery({ topic: "ping", manual: true }) } },
      "PATCH /webhooks/0199a000-0000-7000-8000-0000000000e1": { status: 200, body: { webhook: hook({ enabled: false }) } },
      "GET /webhooks/0199a000-0000-7000-8000-0000000000e1/deliveries": { status: 200, body: { deliveries: log } },
      [`POST /webhooks/0199a000-0000-7000-8000-0000000000e1/deliveries/${log[1]!.id}/redeliver`]: {
        status: 200,
        body: { delivery: delivery({ attempt: 3, manual: true }) },
      },
    });
    await renderAt(WEBHOOKS_PATH);
    const actions = await screen.findByRole("button", { name: "Actions for Team chat" });

    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: "Send a test ping" }));
    expect(await screen.findByText("The ping reached Team chat. The receiver answered 204.")).toBeInTheDocument();

    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: "Turn off" }));
    await waitFor(() => expect(sent.find((s) => s.method === "PATCH")?.body).toMatchObject({ enabled: false, name: "Team chat", projects: ["CHK"] }));

    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: "Delivery log" }));
    const dialog = await screen.findByRole("dialog", { name: "Deliveries to Team chat" });
    const rows = await within(dialog).findAllByRole("listitem");
    expect(rows).toHaveLength(4);
    expect(rows[0]).toHaveTextContent("Test ping");
    expect(rows[0]).toHaveTextContent("by hand");
    expect(rows[1]).toHaveTextContent("Failed");
    expect(rows[1]).toHaveTextContent("answered 503");
    expect(rows[2]).toHaveTextContent("Waiting");
    expect(rows[2]).toHaveTextContent("Next try");
    expect(within(rows[2]!).queryByRole("button")).toBeNull();
    expect(rows[3]).toHaveTextContent("Withheld");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(rows[1]!).getByRole("button", { name: /^Send again/ }));
    expect(await within(dialog).findByText("Sent again: Delivered.")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(actions).toHaveFocus();
  });

  it("asks before deleting or rotating, and shows a rotated secret once", async () => {
    const confirm = vi.fn(() => true);
    vi.stubGlobal("confirm", confirm);
    const sent = webhooks([hook()], {
      "POST /webhooks/0199a000-0000-7000-8000-0000000000e1/rotate-secret": { status: 200, body: { webhook: hook({ secret: "exciter_whs_new" }) } },
      "DELETE /webhooks/0199a000-0000-7000-8000-0000000000e1": { status: 204 },
    });
    await renderAt(WEBHOOKS_PATH);
    const actions = await screen.findByRole("button", { name: "Actions for Team chat" });
    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: "Rotate the secret" }));
    expect(await screen.findByLabelText("Secret for Team chat")).toHaveValue("exciter_whs_new");
    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    expect(await screen.findByText("Team chat was deleted.")).toBeInTheDocument();
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(sent.some((s) => s.method === "DELETE")).toBe(true);
  });

  it("tells a member that only administrators keep webhooks, and asks the API nothing", async () => {
    const sent = webhooks([hook()]);
    await renderAt(WEBHOOKS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText(en.webhooks.notAdmin)).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
    expect(sent.some((s) => s.path === "/webhooks")).toBe(false);
  });
});

describe("topicName and deliveryDetail", () => {
  it("names every topic the API offers, and leaves one this client does not know as it is", () => {
    for (const topic of WEBHOOK_TOPICS) expect(topicName(topic)).not.toBe(topic);
    expect(topicName("*")).toBe("Everything");
    expect(topicName("ping")).toBe("Test ping");
    expect(topicName("project.created")).toBe("project.created");
  });

  it("names an envelope version this client does not know by its number", () => {
    expect(versionName(1)).toBe("Version 1");
    expect(versionName(3)).toBe("3");
  });

  it("says what an attempt came to", () => {
    expect(deliveryDetail(delivery())).toBe("The receiver answered 204.");
    expect(deliveryDetail(delivery({ state: "failed", status: null, error: "The receiver could not be reached." }))).toBe("The receiver could not be reached.");
    expect(deliveryDetail(delivery({ state: "pending", nextAttemptAt: "2026-10-01T09:01:00Z" }))).toMatch(/^Next try /);
  });
});
