import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { PROFILE_PATH } from "@/config";
import { applyLanguage, t } from "@/i18n";
import { applyTimeZone } from "@/lib/format";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
  applyLanguage("");
  applyTimeZone("");
});

const INTERNAL = "Something went wrong on our side. Try again, and quote the request id if it keeps happening.";
const refused: Answer = { status: 500, body: { error: { code: "internal", message: INTERNAL } } };
const withChoice = (locale: Me["user"]["locale"], timezone = ""): Me => ({ ...signedIn, user: { ...signedIn.user, locale, timezone } });

const section = () => within(document.querySelector<HTMLElement>("[data-profile-language]")!);

describe("saving the language and time zone on Your profile", () => {
  it("sends nothing until Save, then shows a refused save and keeps the language that was chosen before", async () => {
    const sent = stubApi({ "PATCH /auth/me": refused });
    await renderAt(PROFILE_PATH);
    expect(section().getByLabelText(t.profile.languageField)).toHaveValue("");

    await userEvent.selectOptions(section().getByLabelText(t.profile.languageField), "de");
    expect(sent.filter((each) => each.method === "PATCH")).toEqual([]);
    await userEvent.click(section().getByRole("button", { name: t.profile.save }));
    const banner = await section().findByRole("alert");
    expect(banner).toHaveTextContent(INTERNAL);
    expect(sent.filter((each) => each.method === "PATCH")).toEqual([{ method: "PATCH", path: "/auth/me", body: { locale: "de", timezone: "" } }]);
    expect(screen.getByRole("heading", { level: 1, name: "Your profile" })).toBeInTheDocument();
    expect(section().queryByRole("status")).not.toBeInTheDocument();
    expect(document.documentElement.lang).toBe("en");
  });

  it("holds the choices while the save is under way, and says so once it is done", async () => {
    let answer: (value: Answer) => void = () => undefined;
    stubApi({ "PATCH /auth/me": () => new Promise<Answer>((resolve) => (answer = resolve)) });
    await renderAt(PROFILE_PATH);
    const language = () => section().getByLabelText(t.profile.languageField);
    expect(language()).toBeEnabled();

    await userEvent.selectOptions(language(), "en");
    await userEvent.click(section().getByRole("button", { name: t.profile.save }));
    await waitFor(() => expect(language()).toBeDisabled());
    expect(section().getByLabelText(t.profile.timezoneField)).toBeDisabled();

    answer({ status: 200, body: withChoice("en") });
    await waitFor(() => expect(document.querySelector("[data-language-saved]")).toHaveTextContent(t.profile.saved));
    expect(language()).toBeEnabled();
    expect(language()).toHaveValue("en");
  });

  it("finds a time zone by part of its name, and says when none has it", async () => {
    stubApi({});
    await renderAt(PROFILE_PATH);
    const search = section().getByLabelText(t.kit.find(t.profile.timezoneField));
    await userEvent.type(search, "new york");
    expect(section().getByRole("option", { name: "America/New York" })).toBeInTheDocument();
    await userEvent.clear(search);
    await userEvent.type(search, "atlantis");
    expect(section().getByText(t.profile.timezoneNone)).toBeInTheDocument();
  });
});
