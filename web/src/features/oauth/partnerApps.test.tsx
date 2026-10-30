import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { INSTALLATION_PATH } from "@/config";
import { answerConfirm, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const anApp = (over: Record<string, unknown> = {}) => ({
  id: "partner-client-1",
  name: "Stator",
  clientUri: "https://stator.example.com",
  keycloakClientId: "stator",
  scopes: ["read"],
  grants: 2,
  createdAt: "2026-10-17T09:00:00Z",
  ...over,
});
const installation = { status: 200, body: { installation: { name: "Demo", oauthConnections: true } } };

describe("partner apps", () => {
  it("registers an app and shows its secret once, with the client id to enter in it", async () => {
    let apps: ReturnType<typeof anApp>[] = [];
    const sent = stubApi({
      "GET /installation": installation,
      "GET /oauth/clients": () => ({ status: 200, body: { clients: apps } }),
      "POST /oauth/clients": () => {
        apps = [anApp({ grants: 0 })];
        return { status: 201, body: { client: { ...apps[0], secret: "exciter_ocs_once" } } };
      },
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    expect(await screen.findByText("No partner app is registered.")).toBeInTheDocument();
    const form = screen.getByRole("form", { name: "Register a partner app" });
    await user.type(within(form).getByLabelText("App name"), "Stator");
    await user.type(within(form).getByLabelText("Keycloak client"), "stator");
    await user.click(within(form).getByRole("button", { name: "Register a partner app" }));

    expect(await screen.findByDisplayValue("exciter_ocs_once")).toBeInTheDocument();
    expect(screen.getByDisplayValue("partner-client-1")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST" && r.path === "/oauth/clients")?.body).toEqual({
      name: "Stator",
      clientUri: "",
      keycloakClientId: "stator",
      scopes: ["read"],
    });
    expect(await axeViolations()).toEqual([]);
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByDisplayValue("exciter_ocs_once")).not.toBeInTheDocument();
  });

  it("lets an app write, gives it a new secret and removes it, asking first", async () => {
    let apps = [anApp()];
    const sent = stubApi({
      "GET /installation": installation,
      "GET /oauth/clients": () => ({ status: 200, body: { clients: apps } }),
      "PATCH /oauth/clients/partner-client-1": () => {
        apps = [anApp({ scopes: ["read", "write"] })];
        return { status: 200, body: { client: apps[0] } };
      },
      "POST /oauth/clients/partner-client-1/rotate-secret": { status: 200, body: { client: anApp({ secret: "exciter_ocs_new" }) } },
      "DELETE /oauth/clients/partner-client-1": () => {
        apps = [];
        return { status: 204 };
      },
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    const list = await screen.findByRole("list", { name: "Partner apps" });
    const row = within(list).getByText("Stator").closest("li")!;
    expect(within(row).getByText("Read only")).toBeInTheDocument();
    expect(within(row).getByText("2 people connected")).toBeInTheDocument();
    expect(within(row).getByText("Exchanges sign-ins of the Keycloak client stator")).toBeInTheDocument();

    await user.click(within(row).getByRole("checkbox", { name: "May change what the person may, not only read" }));
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ scopes: ["read", "write"] });

    await user.click(within(row).getByRole("button", { name: "Give Stator a new secret" }));
    expect(await answerConfirm(true)).toHaveTextContent("Rotate the secret of Stator?");
    expect(await screen.findByDisplayValue("exciter_ocs_new")).toBeInTheDocument();

    await user.click(within(row).getByRole("button", { name: "Remove Stator" }));
    expect(await answerConfirm(true)).toHaveTextContent("Remove app Stator?");
    expect(await screen.findByText("Stator was removed.")).toBeInTheDocument();
  });
});
