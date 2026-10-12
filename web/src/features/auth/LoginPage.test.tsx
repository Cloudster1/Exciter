import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { LAST_ORG_KEY, MEMBERS_PATH } from "@/config";
import { safeNext } from "@/lib/session";
import { renderAt, signedIn, stubApi, unauthorized } from "@/test/app";
import { axeViolations } from "@/test/axe";

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

const membersList = { "GET /users": { status: 200, body: { members: [] } }, "GET /users/requests": { status: 200, body: { requests: [] } } };
const ssoLink = () => screen.getByRole("link", { name: "Sign in with your organization's provider" });

describe("the sign-in page", () => {
  it("is where a visitor without a session lands, remembering where they were going", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    const router = await renderAt(`${MEMBERS_PATH}?tab=x`, { me: null });
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ next: `${MEMBERS_PATH}?tab=x` });
    expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
    expect(document.querySelector("[data-top-bar]")).toBeNull();
  });

  it("sends the browser to the organization's provider, and back to where it was going", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    await renderAt(`/login?next=${encodeURIComponent(MEMBERS_PATH)}`, { me: null });
    await userEvent.type(await screen.findByLabelText("Organization"), " Acme ");
    expect(ssoLink()).toHaveAttribute("href", `/api/v1/auth/oidc/acme/start?next=${encodeURIComponent(MEMBERS_PATH)}`);
  });

  it("asks for the organization before starting, and remembers it once given", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    const router = await renderAt("/login", { me: null });
    await userEvent.click(await screen.findByRole("link", { name: "Sign in with your organization's provider" }));
    expect(await screen.findByText("Enter your organization's short name first.")).toBeInTheDocument();
    expect(screen.getByLabelText("Organization")).toHaveAttribute("aria-invalid", "true");
    expect(router.state.location.pathname).toBe("/login");

    // The page is left for the provider on a real click; here the address is enough.
    const link = ssoLink();
    link.addEventListener("click", (event) => event.preventDefault());
    await userEvent.type(screen.getByLabelText("Organization"), "acme");
    await userEvent.click(link);
    expect(localStorage.getItem(LAST_ORG_KEY)).toBe("acme");
  });

  it("starts with the organization typed last time", async () => {
    localStorage.setItem(LAST_ORG_KEY, "acme");
    stubApi({ "GET /auth/me": unauthorized });
    await renderAt("/login", { me: null });
    expect(await screen.findByLabelText("Organization")).toHaveValue("acme");
  });

  it("starts with the organization the address names over the one typed last time", async () => {
    localStorage.setItem(LAST_ORG_KEY, "acme");
    stubApi({ "GET /auth/me": unauthorized });
    await renderAt("/login?org=beta", { me: null });
    expect(await screen.findByLabelText("Organization")).toHaveValue("beta");
  });

  it("explains why a sign-in through the provider came back refused", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    await renderAt("/login?sso=expired", { me: null });
    expect(await screen.findByRole("alert")).toHaveTextContent("That sign-in took too long or was already used. Start it again.");
  });

  it("tells somebody not yet let in that their request waits for an administrator", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    const router = await renderAt("/login?sso=not_a_member", { me: null });
    const waiting = await screen.findByRole("status");
    expect(within(waiting).getByRole("heading", { name: "Your request is waiting" })).toBeInTheDocument();
    expect(waiting).toHaveTextContent(/waiting for an administrator/i);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(router.state.location.pathname).toBe("/login");
    expect(document.querySelector("[data-top-bar]")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("says something useful for a reason it does not know", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    await renderAt("/login?sso=gremlins", { me: null });
    expect(await screen.findByRole("alert")).toHaveTextContent("Signing in through your identity provider did not work.");
  });

  it("signs in with a password and goes where the person was headed", async () => {
    const sent = stubApi({ "GET /auth/me": unauthorized, "POST /auth/login": { status: 200, body: signedIn }, ...membersList });
    const router = await renderAt(`/login?next=${encodeURIComponent(MEMBERS_PATH)}`, { me: null });
    await userEvent.type(await screen.findByLabelText("Email"), " ada@exciter.test ");
    await userEvent.type(screen.getByLabelText("Password"), "a long bootstrap password");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with password" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(MEMBERS_PATH));
    expect(sent.find((r) => r.path === "/auth/login")?.body).toEqual({ email: "ada@exciter.test", password: "a long bootstrap password" });
    expect(await screen.findByRole("heading", { level: 1, name: "Members" })).toBeInTheDocument();
  });

  it("goes home when nobody said where to", async () => {
    stubApi({ "GET /auth/me": unauthorized, "POST /auth/login": { status: 200, body: signedIn } });
    const router = await renderAt("/login", { me: null });
    await userEvent.type(await screen.findByLabelText("Email"), "ada@exciter.test");
    await userEvent.type(screen.getByLabelText("Password"), "a long bootstrap password");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with password" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByText("No projects yet")).toBeInTheDocument();
  });

  it("shows a wrong password where it was typed, and stays", async () => {
    stubApi({
      "GET /auth/me": unauthorized,
      "POST /auth/login": {
        status: 401,
        body: { error: { code: "invalid_credentials", message: "That email and password do not match an account. Check both and try again." } },
      },
    });
    const router = await renderAt("/login", { me: null });
    await userEvent.type(await screen.findByLabelText("Email"), "ada@exciter.test");
    await userEvent.type(screen.getByLabelText("Password"), "wrong");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That email and password do not match an account.");
    expect(router.state.location.pathname).toBe("/login");
  });

  it("puts a refusal of one field beside it", async () => {
    stubApi({
      "GET /auth/me": unauthorized,
      "POST /auth/login": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Check the form.", fields: { email: "Enter your email address." } } },
      },
    });
    await renderAt("/login", { me: null });
    await userEvent.click(await screen.findByRole("button", { name: "Sign in with password" }));
    expect(await screen.findByText("Enter your email address.")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("sends somebody already signed in on", async () => {
    stubApi(membersList);
    const router = await renderAt(`/login?next=${encodeURIComponent(MEMBERS_PATH)}`);
    await waitFor(() => expect(router.state.location.pathname).toBe(MEMBERS_PATH));
  });

  it("passes axe, with a refusal from the provider shown", async () => {
    stubApi({ "GET /auth/me": unauthorized });
    await renderAt("/login?sso=unreachable", { me: null });
    await screen.findByRole("heading", { name: "Sign in" });
    expect(document.querySelector("[data-login] svg[data-logo]")).toHaveAttribute("aria-hidden", "true");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("where sign-in returns to", () => {
  it("is a path of this application and nothing else", () => {
    expect(safeNext("/settings/members?x=1")).toBe("/settings/members?x=1");
    for (const bad of [undefined, "", "https://evil.test", "//evil.test", "/\\evil.test", "settings", "/login", "/login?next=/"]) {
      expect(safeNext(bad)).toBeUndefined();
    }
  });
});
