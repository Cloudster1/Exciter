import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MEMBERS_PATH } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const owner = {
  userId: "0195f000-0000-7000-8000-000000000001",
  email: "owner@exciter.test",
  name: "Olive Owner",
  role: "owner",
  joinedAt: "2026-09-01T08:00:00Z",
  active: true,
};

const settings = (extra: Parameters<typeof stubApi>[0] = {}) => stubApi({ "GET /users": { status: 200, body: { members: [owner] } }, ...extra });

async function openDialog() {
  await userEvent.click(await screen.findByRole("button", { name: "Add person" }));
  return screen.getByRole("dialog", { name: "Add person" });
}

describe("adding a person with a password", () => {
  it("sends what was typed and shows a made password once, with a way to copy it", async () => {
    const sent = settings({
      "POST /users": {
        status: 201,
        body: {
          member: {
            userId: "0195f000-0000-7000-8000-000000000002",
            email: "ada@exciter.test",
            name: "Ada",
            role: "member",
            newAccount: true,
            password: "Kx7mQ2pRt9WvZa4Nb6Hc",
          },
        },
      },
    });
    await renderAt(MEMBERS_PATH);
    const dialog = await openDialog();
    expect(within(dialog).getByLabelText("Email address")).toHaveFocus();
    await userEvent.type(within(dialog).getByLabelText("Email address"), " ada@exciter.test ");
    await userEvent.type(within(dialog).getByLabelText("Name"), "Ada");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add" }));

    await waitFor(() => expect(sent.some((r) => r.method === "POST" && r.path === "/users")).toBe(true));
    expect(sent.find((r) => r.method === "POST" && r.path === "/users")?.body).toEqual({
      email: "ada@exciter.test",
      name: "Ada",
      role: "member",
      password: "",
    });
    const added = await screen.findByRole("dialog", { name: "Person added" });
    expect(within(added).getByText("Ada was added and can sign in now.")).toBeInTheDocument();
    expect(added.querySelector("[data-new-password]")).toHaveTextContent("Kx7mQ2pRt9WvZa4Nb6Hc");
    expect(within(added).getByText(/shown only now/)).toBeInTheDocument();
    await waitFor(() => expect(within(added).getByRole("button", { name: "Copy password" })).toHaveFocus());
    expect(within(added).queryByLabelText("Email address")).toBeNull();

    await userEvent.click(within(added).getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("button", { name: "Add person" })).toHaveFocus();
    expect(document.querySelector("[data-new-password]")).toBeNull();
  });

  it("does not show a password that was chosen, and says so for a person who already had an account", async () => {
    const sent = settings({
      "POST /users": {
        status: 201,
        body: { member: { userId: "0195f000-0000-7000-8000-000000000003", email: "bob@exciter.test", name: "Bob", role: "guest", newAccount: false } },
      },
    });
    await renderAt(MEMBERS_PATH);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText("Email address"), "bob@exciter.test");
    await userEvent.selectOptions(within(dialog).getByLabelText("Role"), "guest");
    await userEvent.type(within(dialog).getByLabelText("Password"), "a password of my own");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add" }));
    const added = await screen.findByRole("dialog", { name: "Person added" });
    expect(within(added).getByText(/already had an account/)).toBeInTheDocument();
    expect(added.querySelector("[data-new-password]")).toBeNull();
    expect(sent.find((r) => r.method === "POST" && r.path === "/users")?.body).toMatchObject({ role: "guest", password: "a password of my own" });
  });

  it("puts a refusal beside the field it is about", async () => {
    settings({
      "POST /users": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Check the form.",
            fields: { password: "Password must be at least 12 characters. Leave it empty to have one made." },
          },
        },
      },
    });
    await renderAt(MEMBERS_PATH);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText("Email address"), "cy@exciter.test");
    await userEvent.type(within(dialog).getByLabelText("Password"), "short");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add" }));
    expect(await within(dialog).findByText(/at least 12 characters\. Leave it empty/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Password")).toHaveAttribute("aria-invalid", "true");
  });

  it("forgets what was typed when it is cancelled", async () => {
    settings();
    await renderAt(MEMBERS_PATH);
    let dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText("Email address"), "cy@exciter.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    dialog = await openDialog();
    expect(within(dialog).getByLabelText("Email address")).toHaveValue("");
  });

  it("passes axe with the dialog open", async () => {
    settings();
    await renderAt(MEMBERS_PATH);
    await openDialog();
    expect(await axeViolations()).toEqual([]);
  });
});
