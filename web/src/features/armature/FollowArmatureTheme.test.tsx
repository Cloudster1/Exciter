import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureFollow } from "@/api/armature";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { followState } from "./FollowArmatureTheme";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const INVALID = 'Armature\'s theme cannot be used in Exciter: "not a colour" is not a colour. Choose another theme in Armature, then follow it again.';

const account = (over: Partial<ArmatureAccount> = {}): ArmatureAccount => ({
  configured: true,
  source: "token",
  signIn: null,
  baseUrl: "https://armature.example.com",
  connected: true,
  status: "ok",
  user: {
    id: "0195f000-0000-7000-8000-0000000000b1",
    name: "Ada",
    email: "ada@acme.armature.test",
  },
  readOnly: false,
  checkedAt: "2026-10-01T09:00:00Z",
  missing: [],
  ...over,
});

const follow = (over: Partial<ArmatureFollow> = {}): ArmatureFollow => ({
  following: false,
  status: "ok",
  error: "",
  ...over,
});

function stubThemes(answers: Record<string, Answer | (() => Answer)>) {
  return stubApi({
    "GET /themes": { status: 200, body: { themes: [] } },
    "GET /themes/active": { status: 200, body: { theme: null, source: "" } },
    ...answers,
  });
}

const followSwitch = () => screen.getByRole("switch", { name: "Follow my Armature theme" });

describe("following the Armature theme", () => {
  it("says in a sentence what a follower sees, and why when it is not Armature's theme", () => {
    expect(followState(follow())).toMatch(/^Use the theme you chose in Armature here too/);
    expect(followState(follow({ following: true }))).toMatch(/^Exciter shows the theme you use in Armature/);
    expect(followState(follow({ following: true, status: "unreachable" }))).toMatch(/^Armature did not answer in time/);
    expect(followState(follow({ following: true, status: "rejected" }))).toMatch(/Paste a new token under Your account, Armature account\.$/);
    expect(followState(follow({ following: true, error: INVALID }))).toBe(INVALID);
  });

  it("is not offered to somebody without an Armature token", async () => {
    stubThemes({
      "GET /armature/account": {
        status: 200,
        body: {
          account: account({
            connected: false,
            status: "not_connected",
            user: null,
          }),
        },
      },
    });
    await renderAt("/settings/themes");
    expect(await screen.findByRole("heading", { name: "Themes" })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("switch")).toBeNull());
    expect(document.querySelector("[data-armature-theme]")).toBeNull();
  });

  it("follows and stops following from the themes page", async () => {
    let current = follow();
    const sent = stubThemes({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "GET /armature/theme": () => ({ status: 200, body: { follow: current } }),
      "PUT /armature/theme": () => {
        current = follow({ following: true });
        return { status: 200, body: { follow: current } };
      },
      "DELETE /armature/theme": () => {
        current = follow();
        return { status: 204 };
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/themes");
    await waitFor(() => expect(followSwitch()).toBeEnabled());
    expect(followSwitch()).toHaveAttribute("aria-checked", "false");
    expect(await axeViolations()).toEqual([]);

    await user.click(followSwitch());
    await waitFor(() => expect(followSwitch()).toHaveAttribute("aria-checked", "true"));
    expect(document.querySelector("[data-armature-theme-note]")).toHaveTextContent(/^Exciter shows the theme you use in Armature/);
    expect(sent.some((r) => r.method === "PUT" && r.path === "/armature/theme")).toBe(true);
    // Following changes what the page shows, so the active theme is asked again.
    await waitFor(() => expect(sent.filter((r) => r.method === "GET" && r.path === "/themes/active").length).toBeGreaterThan(1));

    await user.click(followSwitch());
    await waitFor(() => expect(followSwitch()).toHaveAttribute("aria-checked", "false"));
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/armature/theme")).toBe(true);
  });

  it("shows Armature's refusal of a theme as the sentence the server wrote", async () => {
    stubThemes({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "GET /armature/theme": { status: 200, body: { follow: follow() } },
      "PUT /armature/theme": {
        status: 422,
        body: { error: { code: "armature_theme_invalid", message: INVALID } },
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/themes");
    await waitFor(() => expect(followSwitch()).toBeEnabled());
    await user.click(followSwitch());
    expect(await screen.findByText(INVALID)).toBeInTheDocument();
    expect(followSwitch()).toHaveAttribute("aria-checked", "false");
  });

  it("tells a follower when Armature did not answer", async () => {
    stubThemes({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "GET /armature/theme": {
        status: 200,
        body: { follow: follow({ following: true, status: "unreachable" }) },
      },
    });
    await renderAt("/settings/themes");
    await waitFor(() => expect(followSwitch()).toHaveAttribute("aria-checked", "true"));
    expect(document.querySelector("[data-armature-theme-note]")).toHaveTextContent(/^Armature did not answer in time/);
    expect(document.querySelector("[data-armature-theme]")).toHaveAttribute("data-armature-theme-status", "unreachable");
  });
});
