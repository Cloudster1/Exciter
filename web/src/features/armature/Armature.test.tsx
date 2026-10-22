import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureConnection } from "@/api/armature";
import { ARMATURE_ACCOUNT_PATH, ARMATURE_SETTINGS_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

// Stator's tests of the same pages, with the account on a page of its own.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://armature.example.com";
const TOKEN = "armature_pat_acme_ada";
const SECRET = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
const REFUSED = "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here.";

function connection(over: Partial<ArmatureConnection> = {}): ArmatureConnection {
  return {
    baseUrl: BASE,
    armatureOrgId: null,
    webhookSecretSet: false,
    webhookUrl: "http://exciter.test/api/v1/armature/webhook",
    webhookTopics: ["issue.created", "issue.updated", "issue.transitioned", "issue.deleted"],
    connected: 0,
    updatedAt: "2026-10-01T08:00:00Z",
    capabilities: ["issue-embed@1", "remote-links@1", "theme-export@1", "webhooks@1"],
    missing: [],
    clientId: "",
    clientSecretSet: false,
    tokenExchange: false,
    exchangeOffered: false,
    ...over,
  };
}

function account(over: Partial<ArmatureAccount> = {}): ArmatureAccount {
  return {
    configured: true,
    baseUrl: BASE,
    connected: false,
    status: "not_connected",
    user: null,
    readOnly: false,
    checkedAt: null,
    missing: [],
    source: "none",
    signIn: null,
    ...over,
  };
}

const connectedAccount = account({
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Ada", email: "ada@acme.armature.test" },
  checkedAt: "2026-10-01T09:00:00Z",
});

describe("connecting Armature as an administrator", () => {
  it("connects an instance with its webhook secret, which never comes back", async () => {
    let current: ArmatureConnection | null = null;
    const sent = stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "PUT /armature/connection": () => {
        current = connection({ webhookSecretSet: true });
        return { status: 200, body: { connection: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(ARMATURE_SETTINGS_PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Armature" })).toBeInTheDocument();
    expect(document.querySelector("[data-copy-status]")).toBeNull();

    await user.type(screen.getByLabelText("Armature address"), `${BASE}/`);
    await user.type(screen.getByLabelText("Webhook secret"), SECRET);
    await user.click(screen.getByRole("button", { name: "Connect Armature" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ baseUrl: `${BASE}/`, webhookSecret: SECRET });
    expect(screen.getByLabelText("Webhook secret")).toHaveValue("");
    expect(screen.getByLabelText("Webhook secret")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    expect(screen.getByLabelText("Webhook address")).toHaveValue("http://exciter.test/api/v1/armature/webhook");
    expect(within(screen.getByRole("list", { name: "Topics" })).getAllByRole("listitem")).toHaveLength(4);
    expect(await axeViolations()).toEqual([]);
  });

  it("keeps the stored secret unless told to forget it, and shows a refusal by its field", async () => {
    const sent = stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ webhookSecretSet: true, connected: 3 }) } },
      "PUT /armature/connection": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { baseUrl: "Armature did not answer at this address. Check the address, and that Armature is running, then save again." },
          },
        },
      },
    });
    const user = userEvent.setup();
    await renderAt(ARMATURE_SETTINGS_PATH);
    expect(await screen.findByText(/3 members have connected their accounts/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Armature did not answer at this address/)).toBeInTheDocument();
    expect(screen.getByLabelText("Armature address")).toHaveAttribute("aria-invalid", "true");
    expect(sent.filter((r) => r.method === "PUT")[0]?.body).toEqual({ baseUrl: BASE });

    await user.click(screen.getByLabelText("Forget the stored secret"));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PUT")).toHaveLength(2));
    expect(sent.filter((r) => r.method === "PUT")[1]?.body).toEqual({ baseUrl: BASE, webhookSecret: "" });
  });

  it("disconnects after asking", async () => {
    let current: ArmatureConnection | null = connection();
    stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "DELETE /armature/connection": () => {
        current = null;
        return { status: 204 };
      },
      "GET /armature/account": { status: 200, body: { account: account({ configured: false, baseUrl: "", status: "not_configured" }) } },
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt(ARMATURE_SETTINGS_PATH);
    await user.click(await screen.findByRole("button", { name: "Disconnect Armature" }));
    expect(confirm).toHaveBeenCalled();
    expect(await screen.findByText("Armature is disconnected.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect Armature" })).toBeInTheDocument();
  });

  it("tells a member the page is for administrators", async () => {
    const sent = stubApi({});
    await renderAt(ARMATURE_SETTINGS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText("Only an administrator can connect Armature.")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/armature/connection")).toBe(false);
  });

  it("says so when the server refuses an administrator who has just lost the role", async () => {
    stubApi({ "GET /armature/connection": { status: 403, body: { error: { code: "forbidden", message: "Only an administrator." } } } });
    await renderAt(ARMATURE_SETTINGS_PATH);
    expect(await screen.findByText("Only an administrator can connect Armature.")).toBeInTheDocument();
  });
});

const LACKS_ISSUES =
  "The connected Armature does not offer issue-embed@1, which Exciter needs to show and find its issues on requirements. Update Armature to a release that offers it, or ask its administrator to.";

describe("an Armature that lacks what Exciter needs", () => {
  it("says so on the settings, naming what goes and what to do", async () => {
    stubApi({
      "GET /armature/connection": {
        status: 200,
        body: { connection: connection({ capabilities: ["webhooks@1"], missing: [{ capability: "issue-embed@1", message: LACKS_ISSUES }] }) },
      },
    });
    await renderAt(ARMATURE_SETTINGS_PATH);
    const note = await screen.findByRole("note", { name: "This Armature needs an update" });
    expect(within(note).getByRole("listitem")).toHaveTextContent(LACKS_ISSUES);
    expect(await axeViolations()).toEqual([]);
  });

  it("says so on a member's account too", async () => {
    stubApi({
      "GET /armature/account": { status: 200, body: { account: { ...connectedAccount, missing: [{ capability: "issue-embed@1", message: LACKS_ISSUES }] } } },
    });
    await renderAt(ARMATURE_ACCOUNT_PATH);
    expect(await screen.findByText(LACKS_ISSUES)).toBeInTheDocument();
  });

  it("shows nothing while Armature offers everything", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: connectedAccount } } });
    await renderAt(ARMATURE_ACCOUNT_PATH);
    await screen.findByText("Connected as Ada (ada@acme.armature.test).");
    expect(screen.queryByRole("note", { name: "This Armature needs an update" })).toBeNull();
  });
});

describe("connecting one's own Armature account", () => {
  it("says to ask an administrator while Exciter has no connection", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account({ configured: false, baseUrl: "", status: "not_configured" }) } } });
    await renderAt(ARMATURE_ACCOUNT_PATH);
    expect(await screen.findByText(/Ask an administrator to connect it under Settings, Armature/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Armature token")).toBeNull();
  });

  it("pastes a token, shows whom it acts as and never the token", async () => {
    let current = account();
    const sent = stubApi({
      "GET /armature/account": () => ({ status: 200, body: { account: current } }),
      "PUT /armature/account/token": () => {
        current = connectedAccount;
        return { status: 200, body: { account: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(ARMATURE_ACCOUNT_PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Armature account" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open Armature/ })).toHaveAttribute("href", BASE);
    await user.type(await screen.findByLabelText("Armature token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Connected as Ada (ada@acme.armature.test).")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ token: TOKEN });
    expect(screen.queryByLabelText("Armature token")).toBeNull();
    expect(document.body).not.toHaveTextContent(TOKEN);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows Armature's refusal of a token as the sentence by the field", async () => {
    stubApi({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "PUT /armature/account/token": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { token: REFUSED } } },
      },
    });
    const user = userEvent.setup();
    await renderAt(ARMATURE_ACCOUNT_PATH);
    await user.type(await screen.findByLabelText("Armature token"), "armature_pat_revoked");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(REFUSED)).toBeInTheDocument();
    expect(screen.getByLabelText("Armature token")).toHaveAttribute("aria-invalid", "true");
  });

  it("says Armature is down in words when it does not answer", async () => {
    stubApi({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "PUT /armature/account/token": {
        status: 502,
        body: {
          error: { code: "armature_unreachable", message: "Armature did not answer. Try again in a moment, and tell an administrator if it keeps happening." },
        },
      },
    });
    const user = userEvent.setup();
    await renderAt(ARMATURE_ACCOUNT_PATH);
    await user.type(await screen.findByLabelText("Armature token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(/Armature did not answer. Try again in a moment/)).toBeInTheDocument();
  });

  it("checks a token, offers a new one when Armature rejects it, and disconnects", async () => {
    let current = connectedAccount;
    stubApi({
      "GET /armature/account": () => ({ status: 200, body: { account: current } }),
      "POST /armature/account/check": () => {
        current = { ...connectedAccount, status: "rejected" };
        return { status: 200, body: { account: current } };
      },
      "DELETE /armature/account/token": () => {
        current = account();
        return { status: 204 };
      },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt(ARMATURE_ACCOUNT_PATH);
    await user.click(await screen.findByRole("button", { name: "Check again" }));
    expect(await screen.findByText(/Armature no longer accepts your token/)).toBeInTheDocument();
    expect(screen.getByLabelText("Paste a new token")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(await screen.findByText("Your Armature account is disconnected.")).toBeInTheDocument();
    expect(await screen.findByLabelText("Armature token")).toBeInTheDocument();
  });

  it("is reached from the settings area's You", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account() } } });
    const user = userEvent.setup();
    const router = await renderAt("/settings");
    const you = document.querySelector<HTMLElement>('[data-settings-sidebar] [data-sidebar-group="you"]')!;
    await user.click(within(you).getByRole("link", { name: "Armature account" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(ARMATURE_ACCOUNT_PATH));
    expect(await screen.findByRole("heading", { level: 1, name: "Armature account" })).toBeInTheDocument();
  });

  it("warns that a read-only token makes no issues or links, and offers to replace it", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: { ...connectedAccount, readOnly: true } } } });
    await renderAt(ARMATURE_ACCOUNT_PATH);
    expect(await screen.findByText(/This token may only read/)).toBeInTheDocument();
    expect(screen.getByLabelText("Paste a new token")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("connecting people to Armature at sign-in", () => {
  it("stores Exciter's client id and secret on the connection, the secret never coming back", async () => {
    let current = connection({ tokenExchange: true, exchangeOffered: true });
    const sent = stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "PUT /armature/connection": () => {
        current = connection({ tokenExchange: true, exchangeOffered: true, clientId: "exciter-app", clientSecretSet: true, updatedAt: "2026-10-02T08:00:00Z" });
        return { status: 200, body: { connection: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(ARMATURE_SETTINGS_PATH);
    expect(await screen.findByText("People are connected to Armature when they sign in.")).toBeInTheDocument();
    await user.type(screen.getByLabelText("Client id at Armature"), "exciter-app");
    await user.type(screen.getByLabelText("Client secret at Armature"), "s3cret");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ baseUrl: BASE, clientId: "exciter-app", clientSecret: "s3cret" });
    expect(screen.getByLabelText("Client id at Armature")).toHaveValue("exciter-app");
    expect(screen.getByLabelText("Client secret at Armature")).toHaveValue("");
    expect(screen.getByLabelText("Client secret at Armature")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    expect(await axeViolations()).toEqual([]);
  });

  it("says when the operator has not switched it on", async () => {
    stubApi({ "GET /armature/connection": { status: 200, body: { connection: connection() } } });
    await renderAt(ARMATURE_SETTINGS_PATH);
    expect(await screen.findByText(/EXCITER_ARMATURE_TOKEN_EXCHANGE/)).toBeInTheDocument();
  });

  it("tells a person connected at sign-in so, and one who was not why", async () => {
    const why = "Armature did not connect you at sign-in: Ada has no Armature account yet. Connect with a token below, or ask Armature's administrator.";
    stubApi({
      "GET /armature/account": {
        status: 200,
        body: { account: account({ signIn: { connected: false, message: why, at: "2026-10-01T09:00:00Z" } }) },
      },
    });
    await renderAt(ARMATURE_ACCOUNT_PATH);
    expect(await screen.findByText(why)).toBeInTheDocument();
    expect(screen.getByLabelText("Armature token")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("shows a token from the sign-in as such, without the sentence of an earlier failure", async () => {
    stubApi({
      "GET /armature/account": {
        status: 200,
        body: { account: { ...connectedAccount, source: "sign_in", signIn: { connected: true, message: "", at: "2026-10-01T09:00:00Z" } } },
      },
    });
    await renderAt(ARMATURE_ACCOUNT_PATH);
    expect(await screen.findByText("Connected when you signed in. Disconnecting lasts until your next sign-in.")).toBeInTheDocument();
    expect(document.querySelector("[data-armature-sign-in]")).toBeNull();
  });
});
