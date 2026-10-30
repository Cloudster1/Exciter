import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ACCESS_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const SSO_TAB = `${ACCESS_PATH}/sso`;

const callbackUrl = "http://localhost:5173/api/v1/auth/oidc/callback";
const stored = {
  issuer: "https://id.test/realms/acme",
  clientId: "exciter",
  hasSecret: true,
  groupsClaim: "groups",
  scopes: "openid profile email",
  createGroups: false,
  admitByGroup: false,
  enabled: true,
  updatedAt: "2026-09-29T08:00:00Z",
};

describe("the single sign-on settings", () => {
  it("sets up a provider with the standard's fields only, and says where people sign in", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl } },
      "PUT /oidc-provider": () => ({ status: 200, body: { provider: { ...stored, updatedAt: "2026-09-29T09:00:00Z" }, callbackUrl } }),
    });
    await renderAt(SSO_TAB);
    await userEvent.type(await screen.findByLabelText("Issuer"), "https://id.test/realms/acme");
    expect(screen.queryByLabelText("Scopes")).toBeNull();
    expect(screen.queryByText(callbackUrl)).toBeNull();
    expect(screen.queryByRole("button", { name: "Turn it off" })).toBeNull();
    await userEvent.type(screen.getByLabelText("Client ID"), "exciter");
    await userEvent.type(screen.getByLabelText("Client secret"), "s3cret");
    await userEvent.click(screen.getByRole("button", { name: "Set up single sign-on" }));

    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      issuer: "https://id.test/realms/acme",
      clientId: "exciter",
      clientSecret: "s3cret",
      groupsClaim: "groups",
      scopes: "openid profile email",
      createGroups: false,
      admitByGroup: false,
      enabled: true,
    });
    expect(await screen.findByText("Single sign-on is saved.")).toBeInTheDocument();
    expect(await screen.findByText(`${window.location.origin}/api/v1/auth/oidc/start`)).toBeInTheDocument();
    expect(document.querySelector("[data-sso-state]")).toHaveTextContent("People can sign in at");
  });

  it("keeps a stored secret, and the scopes and groups dynamo-connect set, when it saves", async () => {
    const connected = { ...stored, scopes: "openid profile email groups", createGroups: true };
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: connected, callbackUrl } },
      "PUT /oidc-provider": { status: 200, body: { provider: connected, callbackUrl } },
    });
    await renderAt(SSO_TAB);
    expect(await screen.findByLabelText("Client secret")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    const body = sent.find((r) => r.method === "PUT")?.body as Record<string, unknown>;
    expect(body).not.toHaveProperty("clientSecret");
    expect(body).toMatchObject({ scopes: "openid profile email groups", createGroups: true, enabled: true });
  });

  it("turns it off, keeping what is stored, and then says where people would sign in", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } },
      "PUT /oidc-provider": { status: 200, body: { provider: { ...stored, enabled: false, updatedAt: "2026-09-29T09:00:00Z" }, callbackUrl } },
    });
    await renderAt(SSO_TAB);
    await userEvent.click(await screen.findByRole("button", { name: "Turn it off" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      issuer: stored.issuer,
      clientId: stored.clientId,
      groupsClaim: stored.groupsClaim,
      scopes: stored.scopes,
      createGroups: false,
      admitByGroup: false,
      enabled: false,
    });
    expect(await screen.findByText("Single sign-on is turned off.")).toBeInTheDocument();
    await waitFor(() => expect(document.querySelector("[data-sso-state]")).toHaveTextContent("Turned off. The address would be"));
    expect(screen.queryByRole("button", { name: "Turn it off" })).toBeNull();
  });

  // Off unless an administrator turns it on, and the page says what it does.
  it("lets people in by their groups only once it is ticked and saved", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } },
      "PUT /oidc-provider": { status: 200, body: { provider: { ...stored, admitByGroup: true, updatedAt: "2026-09-29T09:00:00Z" }, callbackUrl } },
    });
    await renderAt(SSO_TAB);
    const admit = await screen.findByRole("checkbox", { name: "Let people in by their groups" });
    expect(admit).not.toBeChecked();
    expect(admit).toHaveAccessibleDescription(/whose group from the provider holds a grant is let in with it/);

    await userEvent.click(admit);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toMatchObject({ admitByGroup: true, enabled: true, createGroups: false });
    expect(await screen.findByRole("checkbox", { name: "Let people in by their groups" })).toBeChecked();
  });

  it("leaves people in no group waiting when it is left unticked", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: { ...stored, admitByGroup: true }, callbackUrl } },
      "PUT /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } },
    });
    await renderAt(SSO_TAB);
    const admit = await screen.findByRole("checkbox", { name: "Let people in by their groups" });
    expect(admit).toBeChecked();
    await userEvent.click(admit);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toMatchObject({ admitByGroup: false });
  });

  it("shows what the server refused against its field", async () => {
    stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl } },
      "PUT /oidc-provider": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { issuer: "Enter the provider's issuer URL, such as https://id.example.com/realms/acme." },
          },
        },
      },
    });
    await renderAt(SSO_TAB);
    await userEvent.click(await screen.findByRole("button", { name: "Set up single sign-on" }));
    expect(await screen.findByText("Enter the provider's issuer URL, such as https://id.example.com/realms/acme.")).toBeInTheDocument();
    expect(screen.getByLabelText("Issuer")).toHaveAttribute("aria-invalid", "true");
  });

  it("tells a member it is for administrators", async () => {
    stubApi({
      "GET /oidc-provider": {
        status: 403,
        body: { error: { code: "forbidden", message: "Only an administrator can do that. Ask one." } },
      },
    });
    await renderAt(SSO_TAB);
    expect(await screen.findByRole("alert")).toHaveTextContent("Only an administrator changes how people sign in.");
  });

  it("is not even asked for on behalf of a member", async () => {
    const sent = stubApi({});
    await renderAt(SSO_TAB, { me: { ...signedIn, administers: false } });
    expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
    expect(sent.some((r) => r.path.startsWith("/oidc-provider"))).toBe(false);
  });

  it("passes axe", async () => {
    stubApi({ "GET /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } } });
    await renderAt(SSO_TAB);
    await screen.findByLabelText("Issuer");
    expect(await axeViolations()).toEqual([]);
  });
});
