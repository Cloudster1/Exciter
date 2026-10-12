import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { LANGUAGE_STORAGE_KEY } from "@/config";
import { applyLanguage } from "@/i18n";
import { de } from "@/i18n/de";
import { en } from "@/i18n/en";
import { renderAt, signedIn, stubApi } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
  applyLanguage("");
});

const withChoice = (locale: Me["user"]["locale"]): Me => ({ ...signedIn, user: { ...signedIn.user, locale } });

async function pick(menuName: string, item: string) {
  await userEvent.click(screen.getByRole("button", { name: menuName }));
  await userEvent.click(within(screen.getByRole("menu", { name: menuName })).getByRole("menuitemradio", { name: item }));
}

describe("the interface language", () => {
  it("follows the browser while the person has not chosen", async () => {
    stubApi({});
    await renderAt("/");
    expect(document.documentElement.lang).toBe("en");
    expect(screen.getByRole("button", { name: en.account.menu })).toBeInTheDocument();
  });

  it("speaks the language the profile chose", async () => {
    stubApi({});
    await renderAt("/", { me: withChoice("de") });
    await waitFor(() => expect(screen.getByRole("button", { name: de.account.menu })).toBeInTheDocument());
    expect(document.documentElement.lang).toBe("de");
    expect(screen.getByRole("heading", { level: 1, name: de.home.title })).toBeInTheDocument();
  });

  it("is chosen in the account menu, saved, and shown at once", async () => {
    const sent = stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("de") } });
    await renderAt("/");
    await pick(en.account.menu, "Deutsch");

    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: de.home.title })).toBeInTheDocument());
    expect(sent.find((each) => each.method === "PATCH")).toEqual({ method: "PATCH", path: "/auth/me", body: { locale: "de" } });
    expect(document.documentElement.lang).toBe("de");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("de");
    await userEvent.click(screen.getByRole("button", { name: de.account.menu }));
    expect(within(screen.getByRole("menu", { name: de.account.menu })).getByRole("menuitemradio", { name: "Deutsch" })).toHaveAttribute("aria-checked", "true");
  });

  it("goes back to the browser's language when the choice is cleared", async () => {
    stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("") } });
    await renderAt("/", { me: withChoice("de") });
    await waitFor(() => expect(screen.getByRole("button", { name: de.account.menu })).toBeInTheDocument());
    await pick(de.account.menu, de.account.browserLanguage);

    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: en.home.title })).toBeInTheDocument());
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBeNull();
  });
});
