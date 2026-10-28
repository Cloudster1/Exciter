import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ApiToken } from "@/api/tokens";
import { TOKENS_PATH } from "@/config";
import { en } from "@/i18n/en";
import { renderAt, stubApi } from "@/test/app";

// Copying a fresh token, and the sentence that says to copy it by hand when the clipboard refuses (#329).

afterEach(() => vi.unstubAllGlobals());

const secret = "exciter_pat_fresh";

const made: ApiToken = {
  id: "0195f000-0000-7000-8000-0000000005b1",
  name: "Nightly export",
  scopes: [],
  projects: [],
  allProjects: true,
  lastUsedAt: null,
  expiresAt: null,
  createdAt: "2026-10-01T08:00:00Z",
  secret,
};

// user-event puts a clipboard of its own on navigator for each of its calls, so the one a test answers with is set
// after them, just before the click it is for.
function clipboardThat(writeText: (text: string) => Promise<void>) {
  const spy = vi.fn(writeText);
  Object.defineProperty(window.navigator, "clipboard", { configurable: true, value: { writeText: spy } });
  return spy;
}

describe("a fresh token", () => {
  it("is copied when the clipboard takes it, and says to copy it by hand when it does not", async () => {
    let mine: ApiToken[] = [];
    stubApi({
      "GET /tokens": () => ({ status: 200, body: { tokens: mine } }),
      "GET /projects": { status: 200, body: { projects: [] } },
      "POST /tokens": () => {
        mine = [{ ...made, secret: undefined }];
        return { status: 201, body: { token: made } };
      },
    });
    await renderAt(TOKENS_PATH);
    await userEvent.type(await screen.findByRole("textbox", { name: "Token name" }), "Nightly export");
    await userEvent.click(screen.getByRole("button", { name: "Create token" }));
    expect(await screen.findByRole("textbox", { name: "Your new token" })).toHaveValue(secret);
    const copy = screen.getByRole("button", { name: en.tokens.copy });

    const taken = clipboardThat(async () => undefined);
    fireEvent.click(copy);
    expect(await screen.findByText(en.tokens.copied)).toBeInTheDocument();
    expect(taken).toHaveBeenCalledExactlyOnceWith(secret);

    const refused = clipboardThat(async () => {
      throw new DOMException("Write permission denied.", "NotAllowedError");
    });
    fireEvent.click(copy);
    expect(await screen.findByText(en.tokens.copyFailed)).toBeInTheDocument();
    expect(refused).toHaveBeenCalledExactlyOnceWith(secret);
    expect(screen.queryByText(en.tokens.copied)).toBeNull();
  });
});
