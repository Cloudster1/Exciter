import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SSO_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

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
  it("sets up a provider, and shows the address to register with it", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl } },
      "PUT /oidc-provider": (request) => ({
        status: 200,
        body: { provider: { ...stored, updatedAt: "2026-09-29T09:00:00Z" }, callbackUrl, request: request.url },
      }),
    });
    await renderAt(SSO_PATH);
    expect(await screen.findByText(callbackUrl)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Issuer"), "https://id.test/realms/acme");
    await userEvent.type(screen.getByLabelText("Client ID"), "exciter");
    await userEvent.type(screen.getByLabelText("Client secret"), "s3cret");
    await userEvent.click(screen.getByLabelText("Create groups the provider names"));
    await userEvent.click(screen.getByRole("button", { name: "Set up single sign-on" }));

    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      issuer: "https://id.test/realms/acme",
      clientId: "exciter",
      clientSecret: "s3cret",
      groupsClaim: "groups",
      scopes: "openid profile email",
      createGroups: true,
      admitByGroup: false,
      enabled: true,
    });
    expect(await screen.findByText("/api/v1/auth/oidc/start")).toBeInTheDocument();
    // The saved row comes back newer, which starts the form afresh; the word stays.
    expect(await screen.findByRole("status")).toHaveTextContent("Saved.");
  });

  it("keeps a stored secret when the field is left blank", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } },
      "PUT /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } },
    });
    await renderAt(SSO_PATH);
    expect(await screen.findByLabelText("Client secret")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    await userEvent.click(screen.getByLabelText("Members sign in through this provider"));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
    const body = sent.find((r) => r.method === "PUT")?.body as Record<string, unknown>;
    expect(body).not.toHaveProperty("clientSecret");
    expect(body.enabled).toBe(false);
    expect(await screen.findByRole("status")).toHaveTextContent("Saved.");
  });

  // Off unless an administrator turns it on, and the page says what it does.
  it("lets people in by their groups only once it is ticked and saved", async () => {
    const sent = stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } },
      "PUT /oidc-provider": { status: 200, body: { provider: { ...stored, admitByGroup: true, updatedAt: "2026-09-29T09:00:00Z" }, callbackUrl } },
    });
    await renderAt(SSO_PATH);
    const admit = await screen.findByRole("checkbox", { name: "Let people in by their groups" });
    expect(admit).not.toBeChecked();
    expect(admit).toHaveAccessibleDescription(/whose group from the provider is mapped to a role is let in with that role/);

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
    await renderAt(SSO_PATH);
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
    await renderAt(SSO_PATH);
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
    await renderAt(SSO_PATH);
    expect(await screen.findByRole("alert")).toHaveTextContent("Only an administrator changes how people sign in.");
  });

  it("is not even asked for on behalf of a member", async () => {
    const sent = stubApi({});
    await renderAt(SSO_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText("Only an administrator changes how people sign in. Ask one.")).toBeInTheDocument();
    expect(sent.some((r) => r.path.startsWith("/oidc-provider"))).toBe(false);
  });

  it("passes axe", async () => {
    stubApi({ "GET /oidc-provider": { status: 200, body: { provider: stored, callbackUrl } } });
    await renderAt(SSO_PATH);
    await screen.findByLabelText("Issuer");
    expect(await axeViolations()).toEqual([]);
  });
});
