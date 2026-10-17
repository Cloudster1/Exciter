import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { INSTALLATION_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("the installation page", () => {
  it("renames the installation, and the sidebar head follows", async () => {
    let name = "Demo";
    const sent = stubApi({
      "GET /installation": () => ({ status: 200, body: { installation: { name, oauthConnections: true } } }),
      "PUT /installation": async (request) => {
        name = ((await request.json()) as { name: string }).name;
        return { status: 200, body: { installation: { name, oauthConnections: true } } };
      },
      "GET /auth/me": () => ({ status: 200, body: { ...signedIn, installation: { name } } }),
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Installation" })).toBeInTheDocument();
    const field = await screen.findByLabelText("Name");
    expect(field).toHaveValue("Demo");
    expect(await axeViolations()).toEqual([]);

    await user.clear(field);
    await user.type(field, "Acme requirements");
    await user.click(screen.getByRole("button", { name: "Save the name" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ name: "Acme requirements" });
    await waitFor(() => expect(document.querySelector("[data-sidebar-org]")).toHaveTextContent("Acme requirements"));
  });

  it("switches OAuth connections off and says so", async () => {
    const sent = stubApi({
      "GET /installation": () => ({ status: 200, body: { installation: { name: "Demo", oauthConnections: true } } }),
      "PUT /installation/oauth-connections": () => ({ status: 200, body: { installation: { name: "Demo", oauthConnections: false } } }),
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    const toggle = await screen.findByRole("switch", { name: "Let assistants connect with OAuth" });
    expect(toggle).toHaveAttribute("aria-checked", "true");
    await user.click(toggle);
    expect(await screen.findByText("OAuth connections are off.")).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(sent.find((r) => r.path === "/installation/oauth-connections")?.body).toEqual({ enabled: false });
  });

  it("shows the refusal beside the field", async () => {
    stubApi({
      "GET /installation": () => ({ status: 200, body: { installation: { name: "Demo", oauthConnections: true } } }),
      "PUT /installation": () => ({
        status: 422,
        body: { error: { code: "validation_failed", message: "Check the fields.", fields: { name: "Give the installation a name." } } },
      }),
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    await user.clear(await screen.findByLabelText("Name"));
    await user.click(screen.getByRole("button", { name: "Save the name" }));
    expect(await screen.findByText("Give the installation a name.")).toBeInTheDocument();
  });

  it("tells a member to ask an administrator", async () => {
    stubApi({});
    await renderAt(INSTALLATION_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText("Only an administrator can rename the installation. Ask one of them.")).toBeInTheDocument();
  });
});
