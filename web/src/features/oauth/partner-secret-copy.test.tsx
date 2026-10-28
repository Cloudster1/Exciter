import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { INSTALLATION_PATH } from "@/config";
import { en } from "@/i18n/en";
import { renderAt, stubApi } from "@/test/app";

// Copying a partner app's new secret, and the sentence that says to copy it by hand when the clipboard refuses (#329).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const rotated = "exciter_ocs_rotated";

const app = {
  id: "partner-client-1",
  name: "Stator",
  clientUri: "https://stator.example.com",
  keycloakClientId: "stator",
  scopes: ["read"],
  grants: 1,
  createdAt: "2026-10-17T09:00:00Z",
};

// user-event puts a clipboard of its own on navigator for each of its calls, so the one a test answers with is set
// after them, just before the click it is for.
function clipboardThat(writeText: (text: string) => Promise<void>) {
  const spy = vi.fn(writeText);
  Object.defineProperty(window.navigator, "clipboard", { configurable: true, value: { writeText: spy } });
  return spy;
}

describe("a partner app's new secret", () => {
  it("is copied when the clipboard takes it, and says to copy it by hand when it does not", async () => {
    stubApi({
      "GET /installation": { status: 200, body: { installation: { name: "Demo", oauthConnections: true } } },
      "GET /oauth/clients": { status: 200, body: { clients: [app] } },
      [`POST /oauth/clients/${app.id}/rotate-secret`]: { status: 200, body: { client: { ...app, secret: rotated } } },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await renderAt(INSTALLATION_PATH);
    const row = within(await screen.findByRole("list", { name: "Partner apps" }))
      .getByText("Stator")
      .closest("li") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Give Stator a new secret" }));
    expect(await screen.findByLabelText("Secret of Stator")).toHaveValue(rotated);
    const copy = screen.getByRole("button", { name: en.webhooks.copy });

    const taken = clipboardThat(async () => undefined);
    fireEvent.click(copy);
    expect(await screen.findByText(en.webhooks.copied)).toBeInTheDocument();
    expect(taken).toHaveBeenCalledExactlyOnceWith(rotated);

    const refused = clipboardThat(async () => {
      throw new DOMException("Write permission denied.", "NotAllowedError");
    });
    fireEvent.click(copy);
    expect(await screen.findByText(en.webhooks.copyFailed)).toBeInTheDocument();
    expect(refused).toHaveBeenCalledExactlyOnceWith(rotated);
    expect(screen.queryByText(en.webhooks.copied)).toBeNull();
  });
});
