import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { LANGUAGE_STORAGE_KEY, PROFILE_PATH } from "@/config";
import { applyLanguage } from "@/i18n";
import { de } from "@/i18n/de";
import { en } from "@/i18n/en";
import { applyTimeZone, timeZone } from "@/lib/format";
import { renderAt, signedIn, stubApi } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
  applyLanguage("");
  applyTimeZone("");
});

const withChoice = (locale: Me["user"]["locale"], timezone = ""): Me => ({ ...signedIn, user: { ...signedIn.user, locale, timezone } });

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

  it("is chosen on Your profile, saved with its Save, and shown at once", async () => {
    const sent = stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("de") } });
    await renderAt(PROFILE_PATH);
    await userEvent.selectOptions(await screen.findByLabelText(en.profile.languageField), "de");
    expect(sent.find((each) => each.method === "PATCH")).toBeUndefined();
    await userEvent.click(screen.getByRole("button", { name: en.profile.save }));

    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: de.profile.title })).toBeInTheDocument());
    expect(sent.find((each) => each.method === "PATCH")).toEqual({ method: "PATCH", path: "/auth/me", body: { locale: "de", timezone: "" } });
    expect(document.documentElement.lang).toBe("de");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("de");
    expect(screen.getByLabelText(de.profile.languageField)).toHaveValue("de");
    expect(document.querySelector("[data-language-saved]")).toHaveTextContent(de.profile.saved);
  });

  it("goes back to the browser's language when the choice is cleared", async () => {
    stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("") } });
    await renderAt(PROFILE_PATH, { me: withChoice("de") });
    await userEvent.selectOptions(await screen.findByLabelText(de.profile.languageField), "");
    await userEvent.click(screen.getByRole("button", { name: de.profile.save }));

    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: en.profile.title })).toBeInTheDocument());
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBeNull();
  });

  it("saves the time zone with the language, and writes times in it", async () => {
    const sent = stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("", "Asia/Tokyo") } });
    await renderAt(PROFILE_PATH);
    await userEvent.type(await screen.findByLabelText(en.kit.find(en.profile.timezoneField)), "tokyo");
    await userEvent.selectOptions(screen.getByLabelText(en.profile.timezoneField), "Asia/Tokyo");
    expect(document.querySelector("[data-format-preview]")).toHaveTextContent(/A time reads like this/);
    await userEvent.click(screen.getByRole("button", { name: en.profile.save }));

    await waitFor(() => expect(timeZone()).toBe("Asia/Tokyo"));
    expect(sent.find((each) => each.method === "PATCH")).toEqual({ method: "PATCH", path: "/auth/me", body: { locale: "", timezone: "Asia/Tokyo" } });
    expect(await screen.findByLabelText(en.profile.timezoneField)).toHaveValue("Asia/Tokyo");
    expect(document.querySelector("[data-language-saved]")).toHaveTextContent(en.profile.saved);
  });

  it("writes times in the zone the profile chose", async () => {
    stubApi({});
    await renderAt("/", { me: withChoice("", "America/New_York") });
    await waitFor(() => expect(timeZone()).toBe("America/New_York"));
  });
});
