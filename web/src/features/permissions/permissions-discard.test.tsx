import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Grant } from "@/api/permissions";
import { t } from "@/i18n";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const ada = signedIn.user;
const bob = { id: "0195f000-0000-7000-8000-0000000000b2", name: "Bob Builder" };
const saved: Grant[] = [
  { subject: { type: "everyone", id: null, name: "Everyone" }, role: "editor" },
  { subject: { type: "user", id: ada.id, name: ada.name }, role: "admin" },
  { subject: { type: "user", id: bob.id, name: bob.name }, role: "editor" },
];

const rows = () => Array.from(document.querySelectorAll("[data-grant]")).map((row) => row.getAttribute("data-grant"));
// The notice beside the save button, apart from the other live regions of the page.
const status = () => within(document.querySelector('[data-action="save-grants"]')!.parentElement as HTMLElement).getByRole("status");

describe("a project's permissions before they are saved", () => {
  it("drops a removed row and marks the table unsaved, then discards back to what was saved without a request", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/grants": { status: 200, body: { grants: saved } },
    });
    await renderAt("/p/CHK/permissions");
    await screen.findByRole("combobox", { name: t.permissions.roleOf(bob.name) });
    expect(rows()).toEqual(["Everyone", ada.name, bob.name]);
    expect(screen.queryByRole("button", { name: t.permissions.discard })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: t.permissions.save })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: t.permissions.remove(bob.name) }));
    expect(rows()).toEqual(["Everyone", ada.name]);
    expect(status()).toHaveTextContent(t.permissions.unsaved);
    expect(screen.getByRole("button", { name: t.permissions.save })).toBeEnabled();

    await userEvent.selectOptions(screen.getByRole("combobox", { name: t.permissions.roleOf("Everyone") }), "viewer");
    await userEvent.click(screen.getByRole("button", { name: t.permissions.discard }));
    expect(rows()).toEqual(["Everyone", ada.name, bob.name]);
    expect(screen.getByRole("combobox", { name: t.permissions.roleOf("Everyone") })).toHaveValue("editor");
    expect(screen.queryByRole("button", { name: t.permissions.discard })).not.toBeInTheDocument();
    expect(status()).toHaveTextContent("");
    expect(screen.getByRole("button", { name: t.permissions.save })).toBeDisabled();
    expect(sent.filter((request) => request.method !== "GET")).toEqual([]);
  });
});
