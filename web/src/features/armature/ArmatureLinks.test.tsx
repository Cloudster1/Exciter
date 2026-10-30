import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureIssue, ArmatureLink } from "@/api/armature";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://armature.example.com";
const project = aProject();

const connected: ArmatureAccount = {
  configured: true,
  source: "token",
  signIn: null,
  baseUrl: BASE,
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Bob", email: "bob@acme.armature.test" },
  readOnly: false,
  checkedAt: "2026-10-01T09:00:00Z",
  missing: [],
};
const withoutToken: ArmatureAccount = { ...connected, connected: false, status: "not_connected", user: null, checkedAt: null };

function anIssue(key: string, summary: string, category = "in_progress", over: Partial<ArmatureIssue> = {}): ArmatureIssue {
  const names: Record<string, string> = { todo: "To do", in_progress: "In progress", done: "Done" };
  return {
    key,
    summary,
    url: `${BASE}/issues/${key}`,
    type: "Task",
    status: { name: names[category]!, category },
    assigneeName: "Alice",
    updatedAt: "2026-10-01T09:00:00Z",
    ...over,
  };
}

function aLink(issueKey: string, over: Partial<ArmatureLink> = {}): ArmatureLink {
  return {
    id: `0195f000-0000-7000-8000-${issueKey.replace("-", "").padStart(12, "0").toLowerCase()}`,
    via: null,
    relation: "implements",
    issueKey,
    statusCategory: "in_progress",
    statusStale: false,
    sync: "synced",
    reviewedRevision: 1,
    suspect: false,
    error: "",
    brokenAt: null,
    detachedAt: null,
    createdByName: "Bob",
    createdAt: "2026-10-01T09:00:00Z",
    ...over,
  };
}

function stubPage({
  account = connected,
  links = [aLink("CP-1"), aLink("CP-2", { relation: "verifies", statusCategory: "done", sync: "failed", error: "Armature did not let Bob link." })],
  edit = true,
  more = {},
}: {
  account?: ArmatureAccount;
  links?: ArmatureLink[];
  edit?: boolean;
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const requirement = aRequirement({ can: { edit, transition: edit, delete: edit, link: edit, comment: true } });
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /armature/account": { status: 200, body: { account } },
    "GET /requirements/CHK-R1/armature-links": { status: 200, body: { links } },
    "GET /armature/issues": (request) => {
      const keys = new URL(request.url).searchParams.getAll("key");
      const known: Record<string, ArmatureIssue> = { "CP-1": anIssue("CP-1", "Set up the build pipeline"), "CP-2": anIssue("CP-2", "Fix sign-in", "done") };
      return { status: 200, body: { status: "ok", issues: keys.map((key) => ({ key, issue: known[key] ?? null })) } };
    },
    ...more,
  });
}

describe("a requirement's Armature issues", () => {
  it("shows each issue live by relation, with what Armature has not been told yet", async () => {
    stubPage();
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Armature issues" });
    const cp1 = await within(panel).findByRole("link", { name: /Set up the build pipeline/ });
    expect(cp1).toHaveAttribute("href", `${BASE}/issues/CP-1`);
    expect(cp1).toHaveTextContent("In progress");
    expect(cp1).toHaveTextContent("Alice");
    expect(within(panel).getByRole("heading", { name: "Implemented by" })).toBeInTheDocument();
    expect(within(panel).getByRole("heading", { name: "Verified by" })).toBeInTheDocument();
    const failed = panel.querySelector('[data-armature-link="CP-2"]')!;
    expect(failed).toHaveTextContent("Not on the issue");
    expect(failed).toHaveTextContent("Armature did not let Bob link.");
    expect(panel.querySelector('[data-armature-link="CP-1"]')).not.toHaveTextContent("On the issue");
    expect(await axeViolations()).toEqual([]);
  });

  it("marks a trace the requirement moved past, and confirms it against the revision read", async () => {
    const link = aLink("CP-1", { suspect: true, reviewedRevision: 1 });
    const sent = stubPage({
      links: [link],
      more: {
        "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement({ revision: 2 }) } },
        "GET /requirements/CHK-R1/diff": {
          status: 200,
          body: {
            comparison: {
              from: { number: 1, title: "Pay the basket", type: null, authorName: "Ada", createdAt: "2026-10-01T08:00:00Z" },
              to: { number: 2, title: "Pay the basket at once", type: null, authorName: "Ada", createdAt: "2026-10-02T08:00:00Z" },
              title: [],
              statement: [],
              rationale: [],
              attributes: [],
            },
          },
        },
        [`POST /armature-links/${link.id}/clear`]: { status: 200, body: { link: { ...link, suspect: false, reviewedRevision: 2 } } },
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Armature issues" });
    await waitFor(() => expect(panel.querySelector('[data-armature-link="CP-1"]')).toHaveTextContent("Suspect"));
    await userEvent.click(within(panel).getByRole("button", { name: "Check the trace to CP-1" }));
    const dialog = await screen.findByRole("dialog", { name: "Check the trace of CHK-R1 to CP-1" });
    expect(await within(dialog).findByRole("region", { name: "CHK-R1, revision 1 to 2" })).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Why the link still holds"), "CP-1 still covers it.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm link" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ revision: 2, comment: "CP-1 still covers it." }));
  });

  it("shows a reader without a token the key and the stored status, and how to connect", async () => {
    const sent = stubPage({ account: withoutToken, edit: false });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Armature issues" });
    await waitFor(() => expect(panel.querySelector('[data-armature-issue="CP-2"]')).not.toBeNull());
    const chip = panel.querySelector<HTMLElement>('[data-armature-issue="CP-2"]')!;
    expect(chip).toHaveAttribute("data-state", "connect");
    expect(within(chip).getByRole("link", { name: "CP-2" })).toHaveAttribute("href", `${BASE}/issues/CP-2`);
    expect(chip).toHaveTextContent("Done");
    expect(within(chip).getByRole("link", { name: /Connect your Armature account/ })).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/armature/issues")).toBe(false);
    expect(within(panel).queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(chip).not.toHaveTextContent("Moved in Armature since");
  });

  it("tells a reader without a token when the stored status may be out of date", async () => {
    stubPage({ account: withoutToken, edit: false, links: [aLink("CP-1", { statusCategory: "in_progress", statusStale: true })] });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Armature issues" });
    await waitFor(() => expect(panel.querySelector('[data-armature-issue="CP-1"]')).not.toBeNull());
    const chip = panel.querySelector<HTMLElement>('[data-armature-issue="CP-1"]')!;
    expect(chip).toHaveTextContent("In progress");
    expect(chip).toHaveTextContent("Moved in Armature since");
  });

  it("says Armature needs an update when it no longer offers issue reads", async () => {
    stubPage({ links: [aLink("CP-1")], more: { "GET /armature/issues": { status: 200, body: { status: "outdated", issues: [] } } } });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Armature issues" });
    await waitFor(() => expect(panel.querySelector('[data-armature-issue="CP-1"]')).toHaveTextContent("Armature needs an update to show this issue"));
  });

  it("is not there while Exciter has no Armature", async () => {
    stubPage({ account: { ...withoutToken, configured: false, baseUrl: "", status: "not_configured" } });
    await renderAt("/projects/CHK/r/CHK-R1");
    await screen.findByRole("region", { name: "Trace links" });
    expect(screen.queryByRole("region", { name: "Armature issues" })).toBeNull();
  });

  it("traces the requirement to an issue found by its summary, and says why Armature refused another", async () => {
    let links = [aLink("CP-1")];
    const sent = stubPage({
      links,
      more: {
        "GET /requirements/CHK-R1/armature-links": () => ({ status: 200, body: { links } }),
        "GET /armature/search": { status: 200, body: { status: "ok", issues: [anIssue("CP-4", "Rotate the signing keys", "todo")] } },
        "POST /requirements/CHK-R1/armature-links": () => {
          if (links.length > 1) {
            return {
              status: 409,
              body: { error: { code: "armature_link_exists", message: "This requirement is linked to that issue with that relation already." } },
            };
          }
          const made = aLink("CP-4", { relation: "verifies", sync: "pending", statusCategory: "todo" });
          links = [...links, made];
          return { status: 201, body: { link: made } };
        },
      },
    });
    const user = userEvent.setup();
    await renderAt("/projects/CHK/r/CHK-R1");
    const form = await screen.findByRole("form", { name: "Trace to an Armature issue" });
    await user.selectOptions(within(form).getByLabelText("Relation"), "verifies");
    await user.type(within(form).getByRole("combobox", { name: "Issue" }), "signing");
    await user.click(await within(form).findByRole("option", { name: /Rotate the signing keys/ }));
    await user.click(within(form).getByRole("button", { name: "Trace to the issue" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ issueKey: "CP-4", relation: "verifies" }));
    const panel = screen.getByRole("region", { name: "Armature issues" });
    expect(await within(panel).findByText("Waiting for Armature")).toBeInTheDocument();

    await user.type(within(form).getByRole("combobox", { name: "Issue" }), "signing");
    await user.click(await within(form).findByRole("option", { name: /Rotate the signing keys/ }));
    await user.click(within(form).getByRole("button", { name: "Trace to the issue" }));
    expect(await within(form).findByText(/linked to that issue with that relation already/)).toBeInTheDocument();
  });

  it("removes a trace", async () => {
    let links = [aLink("CP-1")];
    stubPage({
      more: {
        "GET /requirements/CHK-R1/armature-links": () => ({ status: 200, body: { links } }),
        [`DELETE /armature-links/${aLink("CP-1").id}`]: () => {
          links = [];
          return { status: 204 };
        },
      },
    });
    const user = userEvent.setup();
    await renderAt("/projects/CHK/r/CHK-R1");
    await user.click(await screen.findByRole("button", { name: "Remove CP-1, Implemented by the issue" }));
    expect(await screen.findByText(/No issue yet/)).toBeInTheDocument();
    expect(screen.getByText("The trace to CP-1 was removed.")).toBeInTheDocument();
  });
});
