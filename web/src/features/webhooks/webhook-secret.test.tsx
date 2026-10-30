import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Webhook } from "@/api/webhooks";
import { WEBHOOKS_PATH } from "@/config";
import { en } from "@/i18n/en";
import { answerConfirm, renderAt, signedIn, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

// Copying a fresh secret and turning down the confirm of a rotation or a deletion (#328).

afterEach(() => vi.unstubAllGlobals());

const hookId = "0199a000-0000-7000-8000-0000000000e1";
const fresh = "exciter_whs_rotated";

const hook = (over: Partial<Webhook> = {}): Webhook => ({
  id: hookId,
  name: "Team chat",
  url: "https://hooks.example.com/exciter",
  topics: ["requirement.revised"],
  projects: [],
  enabled: true,
  version: 1,
  disabledReason: null,
  failures: 0,
  owner: { id: signedIn.user.id, name: "Ada Lovelace" },
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
  ...over,
});

function webhooks() {
  return stubApi({
    "GET /webhooks": { status: 200, body: { webhooks: [hook()] } },
    "GET /projects": { status: 200, body: { projects: [aProject()] } },
    [`POST /webhooks/${hookId}/rotate-secret`]: { status: 200, body: { webhook: hook({ secret: fresh }) } },
    [`DELETE /webhooks/${hookId}`]: { status: 204 },
  });
}

// user-event puts a clipboard of its own on navigator for each of its calls, so the one a test answers with is set
// after them, just before the click it is for.
function clipboardThat(writeText: (text: string) => Promise<void>) {
  const spy = vi.fn(writeText);
  Object.defineProperty(window.navigator, "clipboard", { configurable: true, value: { writeText: spy } });
  return spy;
}

describe("a fresh secret", () => {
  it("is copied when the clipboard takes it, and says to copy it by hand when it does not", async () => {
    webhooks();
    await renderAt(WEBHOOKS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Team chat" }));
    await userEvent.click(screen.getByRole("menuitem", { name: en.webhooks.rotate }));
    await answerConfirm(true);
    expect(await screen.findByLabelText("Secret for Team chat")).toHaveValue(fresh);
    const copy = screen.getByRole("button", { name: en.webhooks.copy });

    const taken = clipboardThat(async () => undefined);
    fireEvent.click(copy);
    expect(await screen.findByText(en.webhooks.copied)).toBeInTheDocument();
    expect(taken).toHaveBeenCalledExactlyOnceWith(fresh);

    const refused = clipboardThat(async () => {
      throw new DOMException("Write permission denied.", "NotAllowedError");
    });
    fireEvent.click(copy);
    expect(await screen.findByText(en.webhooks.copyFailed)).toBeInTheDocument();
    expect(refused).toHaveBeenCalledExactlyOnceWith(fresh);
    expect(screen.queryByText(en.webhooks.copied)).toBeNull();
  });
});

describe("turning the confirm down", () => {
  it("sends neither a rotation nor a deletion, and leaves the webhook as it was", async () => {
    const sent = webhooks();
    await renderAt(WEBHOOKS_PATH);
    const actions = await screen.findByRole("button", { name: "Actions for Team chat" });
    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: en.webhooks.rotate }));
    expect(await answerConfirm(false)).toHaveTextContent(en.webhooks.confirmRotate);
    await userEvent.click(actions);
    await userEvent.click(screen.getByRole("menuitem", { name: en.webhooks.delete }));
    expect(await answerConfirm(false)).toHaveTextContent(en.webhooks.confirmDelete);

    expect(sent.filter((s) => s.method !== "GET")).toEqual([]);
    expect(screen.queryByLabelText("Secret for Team chat")).toBeNull();
    expect(screen.queryByText(en.webhooks.deleted("Team chat"))).toBeNull();
    expect(screen.getByRole("button", { name: "Actions for Team chat" })).toBeInTheDocument();
  });
});
