import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { OAUTH_CONSENT_PATH } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { leaveTo } from "@/lib/leave";

vi.mock("@/lib/leave", () => ({ leaveTo: vi.fn() }));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

const id = "0195f000-0000-4000-8000-0000000000aa";
const asked = (readOnly: boolean) => ({
  request: {
    id,
    client: { id: "c1", name: "Desk assistant", uri: "https://assistant.example", kind: "registered" },
    redirectHost: "127.0.0.1:33418",
    readOnly,
    expiresAt: "2026-10-17T12:10:00Z",
  },
});
const projects = {
  projects: [
    { id: "p1", key: "CHK", name: "Checkout" },
    { id: "p2", key: "PAY", name: "Payments" },
  ],
};

describe("the consent page", () => {
  it("names the app and where it sends back, and allows it narrowed to reading and a project", async () => {
    const sent = stubApi({
      [`GET /oauth/requests/${id}`]: () => ({ status: 200, body: asked(false) }),
      [`POST /oauth/requests/${id}`]: () => ({ status: 200, body: { redirectTo: "http://127.0.0.1:33418/cb?code=x&state=s" } }),
      "GET /projects": () => ({ status: 200, body: projects }),
    });
    const user = userEvent.setup();
    await renderAt(`${OAUTH_CONSENT_PATH}?request=${id}`);
    expect(await screen.findByText("Desk assistant wants to work in Exciter as you.")).toBeInTheDocument();
    expect(screen.getByText("https://assistant.example")).toBeInTheDocument();
    expect(screen.getByText("You go back to 127.0.0.1:33418 afterwards.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await user.click(screen.getByLabelText("Read only"));
    await user.click(await screen.findByRole("button", { name: /Checkout/ }));
    await user.click(screen.getByRole("button", { name: "Allow" }));

    expect(await screen.findByText("Taking you back to the app.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ allow: true, readOnly: true, projects: ["CHK"] });
    expect(leaveTo).toHaveBeenCalledWith("http://127.0.0.1:33418/cb?code=x&state=s");
  });

  it("keeps an app that asked to read to reading, and denies it", async () => {
    const sent = stubApi({
      [`GET /oauth/requests/${id}`]: () => ({ status: 200, body: asked(true) }),
      [`POST /oauth/requests/${id}`]: () => ({ status: 200, body: { redirectTo: "http://127.0.0.1:33418/cb?error=access_denied" } }),
    });
    const user = userEvent.setup();
    await renderAt(`${OAUTH_CONSENT_PATH}?request=${id}`);
    const box = await screen.findByLabelText("Read only");
    expect(box).toBeChecked();
    expect(box).toBeDisabled();
    expect(screen.getByText("It asked only to read.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Deny" }));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ allow: false, readOnly: false });
    expect(leaveTo).toHaveBeenCalledWith("http://127.0.0.1:33418/cb?error=access_denied");
  });

  it("says when the request ran out, and why an app was refused before it came here", async () => {
    stubApi({ [`GET /oauth/requests/${id}`]: () => ({ status: 404, body: { error: { code: "not_found", message: "Gone." } } }) });
    await renderAt(`${OAUTH_CONSENT_PATH}?request=${id}`);
    expect(await screen.findByText(/This request to connect has expired or was answered already/)).toBeInTheDocument();
  });

  it("explains a refusal the API sent the browser here with", async () => {
    stubApi({});
    await renderAt(`${OAUTH_CONSENT_PATH}?error=invalid_redirect`);
    expect(await screen.findByText(/asked to send you back to an address it never registered/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});
