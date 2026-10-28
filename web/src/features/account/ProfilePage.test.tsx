import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { PROFILE_PATH } from "@/config";
import { applyLanguage, t } from "@/i18n";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
  applyLanguage("");
});

const INTERNAL = "Something went wrong on our side. Try again, and quote the request id if it keeps happening.";
const refused: Answer = { status: 500, body: { error: { code: "internal", message: INTERNAL } } };
const withChoice = (locale: Me["user"]["locale"]): Me => ({ ...signedIn, user: { ...signedIn.user, locale } });

const choice = () => within(document.querySelector<HTMLElement>("[data-language-choice]")!);
const saveStatus = () => within(document.querySelector<HTMLElement>("[data-profile]")!).getByRole("status");

describe("saving the language on Your profile", () => {
  it("shows a refused save and keeps the language that was chosen before", async () => {
    const sent = stubApi({ "PATCH /auth/me": refused });
    await renderAt(PROFILE_PATH);
    expect(choice().getByRole("radio", { name: t.account.browserLanguage })).toBeChecked();

    await userEvent.click(choice().getByRole("radio", { name: "Deutsch" }));
    const banner = await within(document.querySelector<HTMLElement>("[data-profile]")!).findByRole("alert");
    expect(banner).toHaveTextContent(INTERNAL);
    expect(sent.filter((each) => each.method === "PATCH")).toEqual([{ method: "PATCH", path: "/auth/me", body: { locale: "de" } }]);
    expect(choice().getByRole("radio", { name: t.account.browserLanguage })).toBeChecked();
    expect(choice().getByRole("radio", { name: "Deutsch" })).not.toBeChecked();
    expect(screen.getByRole("heading", { level: 1, name: "Your profile" })).toBeInTheDocument();
    expect(saveStatus()).toHaveTextContent("");
    expect(document.documentElement.lang).toBe("en");
  });

  it("holds the choices while the save is under way, and says Saved once it is done", async () => {
    let answer: (value: Answer) => void = () => undefined;
    stubApi({ "PATCH /auth/me": () => new Promise<Answer>((resolve) => (answer = resolve)) });
    await renderAt(PROFILE_PATH);
    const radios = () => choice().getAllByRole("radio");
    expect(radios().every((radio) => !(radio as HTMLInputElement).disabled)).toBe(true);

    await userEvent.click(choice().getByRole("radio", { name: "English" }));
    await waitFor(() => expect(radios().every((radio) => (radio as HTMLInputElement).disabled)).toBe(true));
    expect(radios()).toHaveLength(3);
    expect(saveStatus()).toHaveTextContent("");

    answer({ status: 200, body: withChoice("en") });
    await waitFor(() => expect(saveStatus()).toHaveTextContent(t.profile.saved));
    expect(radios().every((radio) => !(radio as HTMLInputElement).disabled)).toBe(true);
    expect(choice().getByRole("radio", { name: "English" })).toBeChecked();
  });
});
