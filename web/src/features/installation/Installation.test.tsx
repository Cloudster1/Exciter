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
  it("renames the installation, and the breadcrumbs follow", async () => {
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
    await waitFor(() => expect(screen.getByRole("link", { name: "Acme requirements" })).toHaveAttribute("href", "/"));
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

  it("adds the suite demo once confirmed, follows it while it is built, and links it again", async () => {
    const demo = (state: string, kind = "build") => ({
      state,
      kind,
      taken: "2026-10-20",
      projectKeys: ["ARM", "EXC", "STA"],
      step: state === "done" ? 40 : 0,
      steps: 40,
      requirements: state === "done" ? 1540 : 0,
      links: state === "done" ? 12 : 0,
      skipped: state === "done" ? 3 : 0,
      message: state === "done" ? "Your Armature account is not connected, so the demo links nothing there yet." : "",
      createdAt: "2026-10-20T08:00:00Z",
      startedAt: null,
      finishedAt: null,
    });
    let current: ReturnType<typeof demo> | null = null;
    const sent = stubApi({
      "GET /installation": () => ({ status: 200, body: { installation: { name: "Demo", oauthConnections: true } } }),
      "GET /installation/suite-demo": () => ({ status: 200, body: { demo: current } }),
      "POST /installation/suite-demo": () => {
        current = demo("queued");
        return { status: 202, body: { demo: current } };
      },
      "POST /installation/suite-demo/links": () => {
        current = demo("queued", "links");
        return { status: 202, body: { demo: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    await user.click(await screen.findByRole("button", { name: "Add the suite demo" }));
    expect(await screen.findByRole("dialog", { name: "Add the suite demo?" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await user.click(screen.getByRole("button", { name: "Add it" }));
    expect(await screen.findByText("The demo is being built: step 1 of 40. This takes a few minutes.")).toBeInTheDocument();
    expect(sent.filter((r) => r.method === "POST" && r.path === "/installation/suite-demo")).toHaveLength(1);

    current = demo("done");
    expect(await screen.findByText("The demo holds 1540 requirements and 12 links; 3 links wait.", {}, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.getByText(/Your Armature account is not connected/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "EXC" })).toHaveAttribute("href", "/p/EXC");
    expect(screen.queryByRole("button", { name: "Add the suite demo" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Link the demo again" }));
    expect(await screen.findByText("The demo is being linked to Armature and Stator.")).toBeInTheDocument();
  });

  it("says which key keeps the suite demo out", async () => {
    stubApi({
      "GET /installation": () => ({ status: 200, body: { installation: { name: "Demo", oauthConnections: true } } }),
      "GET /installation/suite-demo": () => ({ status: 200, body: { demo: null } }),
      "POST /installation/suite-demo": () => ({
        status: 422,
        body: { error: { code: "validation_failed", message: "Check the fields.", fields: { key: "The key EXC is taken by another project." } } },
      }),
    });
    const user = userEvent.setup();
    await renderAt(INSTALLATION_PATH);
    await user.click(await screen.findByRole("button", { name: "Add the suite demo" }));
    await user.click(await screen.findByRole("button", { name: "Add it" }));
    expect(await screen.findByText("The key EXC is taken by another project.")).toBeInTheDocument();
  });

  it("tells a member to ask an administrator", async () => {
    stubApi({});
    await renderAt(INSTALLATION_PATH, { me: { ...signedIn, administers: false } });
    expect(await screen.findByText("Only an administrator can rename the installation. Ask one of them.")).toBeInTheDocument();
  });
});
