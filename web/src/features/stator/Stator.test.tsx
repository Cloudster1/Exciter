import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { StatorAccount, StatorConnection } from "@/api/stator";
import { STATOR_ACCOUNT_PATH, STATOR_SETTINGS_PATH } from "@/config";
import { answerConfirm, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

// The Armature pages' tests again, for Stator's connection and account (#40).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://stator.example.com";
const TOKEN = "stator_pat_acme_ada";
const SECRET = "stator_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
const REFUSED = "Stator did not accept this token. Make a new one under Your account, Access tokens in Stator and paste it here.";

function connection(over: Partial<StatorConnection> = {}): StatorConnection {
  return {
    baseUrl: BASE,
    remoteLinks: false,
    webhookSecretSet: false,
    webhookUrl: "http://exciter.test/api/v1/stator/webhook",
    webhookTopics: ["page.published", "page.moved", "page.deleted", "page.remote_link.removed"],
    connected: 0,
    updatedAt: "2026-10-01T08:00:00Z",
    clientId: "",
    clientSecretSet: false,
    tokenExchange: false,
    exchangeOffered: false,
    ...over,
  };
}

function account(over: Partial<StatorAccount> = {}): StatorAccount {
  return { configured: true, baseUrl: BASE, connected: false, status: "not_connected", user: null, checkedAt: null, source: "none", signIn: null, ...over };
}

const connectedAccount = account({
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000c1", name: "Ada", email: "ada@acme.stator.test" },
  checkedAt: "2026-10-01T09:00:00Z",
});

describe("connecting Stator as an administrator", () => {
  it("connects an instance with its webhook secret, which never comes back", async () => {
    let current: StatorConnection | null = null;
    const sent = stubApi({
      "GET /stator/connection": () => ({ status: 200, body: { connection: current } }),
      "PUT /stator/connection": () => {
        current = connection({ webhookSecretSet: true });
        return { status: 200, body: { connection: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_SETTINGS_PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Stator" })).toBeInTheDocument();

    await user.type(screen.getByLabelText("Stator address"), `${BASE}/`);
    await user.type(screen.getByLabelText("Webhook secret"), SECRET);
    await user.click(screen.getByRole("button", { name: "Connect Stator" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ baseUrl: `${BASE}/`, webhookSecret: SECRET });
    expect(screen.getByLabelText("Webhook secret")).toHaveValue("");
    expect(screen.getByLabelText("Webhook secret")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    expect(screen.getByLabelText("Webhook address")).toHaveValue("http://exciter.test/api/v1/stator/webhook");
    expect(within(screen.getByRole("list", { name: "Topics" })).getAllByRole("listitem")).toHaveLength(4);
    expect(screen.getByText(/does not show linked requirements on its pages yet/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says when Stator shows the linked requirements on its pages", async () => {
    stubApi({ "GET /stator/connection": { status: 200, body: { connection: connection({ remoteLinks: true, connected: 1 }) } } });
    await renderAt(STATOR_SETTINGS_PATH);
    expect(await screen.findByText(/1 member has connected their account. Stator shows the requirements linked to each page./)).toBeInTheDocument();
  });

  it("keeps the stored secret unless told to forget it, and shows a refusal by its field", async () => {
    const sent = stubApi({
      "GET /stator/connection": { status: 200, body: { connection: connection({ webhookSecretSet: true, connected: 3 }) } },
      "PUT /stator/connection": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { baseUrl: "Something answers at this address, but it is not Stator. Check the address, and leave out any path such as /api/v1." },
          },
        },
      },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_SETTINGS_PATH);
    expect(await screen.findByText(/3 members have connected their accounts/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/but it is not Stator/)).toBeInTheDocument();
    expect(screen.getByLabelText("Stator address")).toHaveAttribute("aria-invalid", "true");
    expect(sent.filter((r) => r.method === "PUT")[0]?.body).toEqual({ baseUrl: BASE });

    await user.click(screen.getByLabelText("Forget the stored secret"));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PUT")).toHaveLength(2));
    expect(sent.filter((r) => r.method === "PUT")[1]?.body).toEqual({ baseUrl: BASE, webhookSecret: "" });
  });

  it("disconnects after asking", async () => {
    let current: StatorConnection | null = connection();
    stubApi({
      "GET /stator/connection": () => ({ status: 200, body: { connection: current } }),
      "DELETE /stator/connection": () => {
        current = null;
        return { status: 204 };
      },
      "GET /stator/account": { status: 200, body: { account: account({ configured: false, baseUrl: "", status: "not_configured" }) } },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_SETTINGS_PATH);
    await user.click(await screen.findByRole("button", { name: "Disconnect Stator" }));
    expect(await answerConfirm(true)).toHaveTextContent("Disconnect Stator?");
    expect(await screen.findByText("Stator is disconnected.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect Stator" })).toBeInTheDocument();
  });

  it("tells a member the page is for administrators", async () => {
    const sent = stubApi({});
    await renderAt(STATOR_SETTINGS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText("Only an administrator can connect Stator.")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/stator/connection")).toBe(false);
  });
});

describe("connecting one's own Stator account", () => {
  it("says to ask an administrator while Exciter has no connection", async () => {
    stubApi({ "GET /stator/account": { status: 200, body: { account: account({ configured: false, baseUrl: "", status: "not_configured" }) } } });
    await renderAt(STATOR_ACCOUNT_PATH);
    expect(await screen.findByText(/Ask an administrator to connect it under Settings, Stator/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Stator token")).toBeNull();
  });

  it("pastes a token, shows whom it acts as and never the token", async () => {
    let current = account();
    const sent = stubApi({
      "GET /stator/account": () => ({ status: 200, body: { account: current } }),
      "PUT /stator/account/token": () => {
        current = connectedAccount;
        return { status: 200, body: { account: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_ACCOUNT_PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Stator account" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open Stator/ })).toHaveAttribute("href", BASE);
    await user.type(await screen.findByLabelText("Stator token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Connected as Ada (ada@acme.stator.test).")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ token: TOKEN });
    expect(screen.queryByLabelText("Stator token")).toBeNull();
    expect(document.body).not.toHaveTextContent(TOKEN);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows Stator's refusal of a token as the sentence by the field", async () => {
    stubApi({
      "GET /stator/account": { status: 200, body: { account: account() } },
      "PUT /stator/account/token": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { token: REFUSED } } },
      },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_ACCOUNT_PATH);
    await user.type(await screen.findByLabelText("Stator token"), "stator_pat_revoked");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(REFUSED)).toBeInTheDocument();
    expect(screen.getByLabelText("Stator token")).toHaveAttribute("aria-invalid", "true");
  });

  it("checks a token, offers a new one when Stator rejects it, and disconnects", async () => {
    let current = connectedAccount;
    const sent = stubApi({
      "GET /stator/account": () => ({ status: 200, body: { account: current } }),
      "POST /stator/account/check": () => {
        current = { ...connectedAccount, status: "rejected" };
        return { status: 200, body: { account: current } };
      },
      "DELETE /stator/account/token": () => {
        current = account();
        return { status: 204 };
      },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_ACCOUNT_PATH);
    await user.click(await screen.findByRole("button", { name: "Check again" }));
    expect(await screen.findByText(/Stator no longer accepts your token/)).toBeInTheDocument();
    expect(screen.getByLabelText("Paste a new token")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(await answerConfirm(true)).toHaveTextContent("Disconnect your Stator account?");
    expect(await screen.findByText("Your Stator account is disconnected.")).toBeInTheDocument();
    expect(await screen.findByLabelText("Stator token")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/stator/account/token")).toBe(true);
  });

  it("is reached from the settings area's You", async () => {
    stubApi({ "GET /stator/account": { status: 200, body: { account: account() } } });
    const user = userEvent.setup();
    const router = await renderAt("/settings");
    const you = document.querySelector<HTMLElement>('[data-settings-sidebar] [data-sidebar-group="you"]')!;
    await user.click(within(you).getByRole("link", { name: "Stator account" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(STATOR_ACCOUNT_PATH));
    expect(await screen.findByRole("heading", { level: 1, name: "Stator account" })).toBeInTheDocument();
  });
});

describe("connecting people to Stator at sign-in", () => {
  it("stores Exciter's client id and secret on the connection, the secret never coming back", async () => {
    let current = connection({ tokenExchange: true, exchangeOffered: true });
    const sent = stubApi({
      "GET /stator/connection": () => ({ status: 200, body: { connection: current } }),
      "PUT /stator/connection": () => {
        current = connection({ tokenExchange: true, exchangeOffered: true, clientId: "exciter-app", clientSecretSet: true, updatedAt: "2026-10-02T08:00:00Z" });
        return { status: 200, body: { connection: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(STATOR_SETTINGS_PATH);
    expect(await screen.findByText("People are connected to Stator when they sign in.")).toBeInTheDocument();
    await user.type(screen.getByLabelText("Client id at Stator"), "exciter-app");
    await user.type(screen.getByLabelText("Client secret at Stator"), "s3cret");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ baseUrl: BASE, clientId: "exciter-app", clientSecret: "s3cret" });
    expect(screen.getByLabelText("Client id at Stator")).toHaveValue("exciter-app");
    expect(screen.getByLabelText("Client secret at Stator")).toHaveValue("");
    expect(screen.getByLabelText("Client secret at Stator")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    expect(await axeViolations()).toEqual([]);
  });

  it("says when the operator has not switched it on", async () => {
    stubApi({ "GET /stator/connection": { status: 200, body: { connection: connection() } } });
    await renderAt(STATOR_SETTINGS_PATH);
    expect(await screen.findByText(/EXCITER_STATOR_TOKEN_EXCHANGE/)).toBeInTheDocument();
  });

  it("tells a person connected at sign-in so, and one who was not why", async () => {
    const why = "Stator did not connect you at sign-in: Ada has no Stator account yet. Connect with a token below, or ask Stator's administrator.";
    stubApi({
      "GET /stator/account": {
        status: 200,
        body: { account: account({ signIn: { connected: false, message: why, at: "2026-10-01T09:00:00Z" } }) },
      },
    });
    await renderAt(STATOR_ACCOUNT_PATH);
    expect(await screen.findByText(why)).toBeInTheDocument();
    expect(screen.getByLabelText("Stator token")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("shows a token from the sign-in as such, without the sentence of an earlier failure", async () => {
    stubApi({
      "GET /stator/account": {
        status: 200,
        body: { account: { ...connectedAccount, source: "sign_in", signIn: { connected: true, message: "", at: "2026-10-01T09:00:00Z" } } },
      },
    });
    await renderAt(STATOR_ACCOUNT_PATH);
    expect(await screen.findByText("Connected when you signed in. Disconnecting lasts until your next sign-in.")).toBeInTheDocument();
    expect(document.querySelector("[data-stator-sign-in]")).toBeNull();
  });
});
