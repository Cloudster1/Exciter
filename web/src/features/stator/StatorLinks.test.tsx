import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { StatorAccount, StatorLink, StatorPage } from "@/api/stator";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// The Armature panel's tests again, for the pages a requirement is traced to (#41).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://stator.example.com";
const TIMING = "0195f000-0000-7000-8000-00000000a001";
const ONBOARDING = "0195f000-0000-7000-8000-00000000a002";
const THREAT = "0195f000-0000-7000-8000-00000000a003";
const project = aProject();

const connected: StatorAccount = {
  configured: true,
  source: "token",
  signIn: null,
  baseUrl: BASE,
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000c1", name: "Bob", email: "bob@acme.stator.test" },
  checkedAt: "2026-10-01T09:00:00Z",
};
const withoutToken: StatorAccount = { ...connected, connected: false, status: "not_connected", user: null, checkedAt: null };

function aPage(id: string, title: string, version: number): StatorPage {
  return {
    id,
    title,
    spaceKey: "ENG",
    spaceName: "Engineering",
    path: ["Engineering home", "Braking system"],
    version,
    url: `${BASE}/s/ENG/p/${id}`,
    updatedAt: "2026-10-01T09:00:00Z",
    updatedByName: "Alice",
  };
}

function aLink(pageId: string, over: Partial<StatorLink> = {}): StatorLink {
  return {
    id: `0195f000-0000-7000-8000-${pageId.slice(-6).padStart(12, "b")}`,
    via: null,
    relation: "specifies",
    pageId,
    spaceKey: "ENG",
    url: `${BASE}/s/ENG/p/${pageId}`,
    pageVersion: 2,
    pageVersionSeen: 2,
    suspect: false,
    reviewedRevision: 1,
    sync: "none",
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
  links = [aLink(TIMING), aLink(ONBOARDING, { relation: "describes", brokenAt: "2026-10-02T09:00:00Z" })],
  edit = true,
  more = {},
}: {
  account?: StatorAccount;
  links?: StatorLink[];
  edit?: boolean;
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const requirement = aRequirement({ can: { edit, transition: edit, delete: edit, link: edit, comment: true } });
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /stator/account": { status: 200, body: { account } },
    "GET /requirements/CHK-R1/stator-links": { status: 200, body: { links } },
    "GET /stator/pages": (request) => {
      const ids = new URL(request.url).searchParams.getAll("id");
      const known: Record<string, StatorPage> = { [TIMING]: aPage(TIMING, "Brake timing", 2) };
      return { status: 200, body: { status: "ok", pages: ids.map((id) => ({ id, page: known[id] ?? null })) } };
    },
    ...more,
  });
}

describe("a requirement's Stator pages", () => {
  it("shows each page live by relation, and a page Stator deleted", async () => {
    stubPage();
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Stator pages" });
    const timing = await within(panel).findByRole("link", { name: /Brake timing/ });
    expect(timing).toHaveAttribute("href", `${BASE}/s/ENG/p/${TIMING}`);
    expect(timing).toHaveTextContent("Engineering home / Braking system");
    expect(timing).toHaveTextContent("Version 2");
    expect(within(panel).getByRole("heading", { name: "Specified by" })).toBeInTheDocument();
    expect(within(panel).getByRole("heading", { name: "Described by" })).toBeInTheDocument();
    const gone = panel.querySelector(`[data-stator-link="${ONBOARDING}"]`)!;
    expect(gone).toHaveTextContent("Deleted in Stator");
    expect(gone.querySelector("[data-state]")).toHaveAttribute("data-state", "hidden");
    expect(await axeViolations()).toEqual([]);
  });

  it("says how far each page has come in showing the requirement, and why Stator refused one", async () => {
    stubPage({
      links: [
        aLink(TIMING, { sync: "synced" }),
        aLink(ONBOARDING, { relation: "describes", sync: "failed", error: "Stator did not let Bob put requirements on this page." }),
        aLink(THREAT, { relation: "relates", sync: "pending" }),
      ],
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Stator pages" });
    await waitFor(() => expect(panel.querySelector(`[data-stator-link="${ONBOARDING}"]`)).toHaveTextContent("Not on the page"));
    expect(panel.querySelector(`[data-stator-link="${ONBOARDING}"]`)).toHaveTextContent("Stator did not let Bob put requirements on this page.");
    expect(panel.querySelector(`[data-stator-link="${THREAT}"]`)).toHaveTextContent("Waiting for Stator");
    expect(panel.querySelector(`[data-stator-link="${TIMING}"]`)).not.toHaveTextContent("On the page");
    expect(await axeViolations()).toEqual([]);
  });

  it("marks a trace whose page has a newer version, and confirms it against that version", async () => {
    const link = aLink(TIMING, { suspect: true, pageVersion: 3, pageVersionSeen: 2 });
    const sent = stubPage({
      links: [link],
      more: { [`POST /stator-links/${link.id}/clear`]: { status: 200, body: { link: { ...link, suspect: false, pageVersionSeen: 3 } } } },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Stator pages" });
    await waitFor(() => expect(panel.querySelector(`[data-stator-link="${TIMING}"]`)).toHaveTextContent("Suspect"));
    await userEvent.click(await within(panel).findByRole("button", { name: "Check the trace to Brake timing" }));
    const dialog = await screen.findByRole("dialog", { name: "Check the trace of CHK-R1 to Brake timing" });
    expect(dialog).toHaveTextContent("from version 2 to version 3");
    expect(within(dialog).getByRole("link", { name: /Open the page in Stator/ })).toHaveAttribute("href", link.url);
    await userEvent.type(within(dialog).getByLabelText("Why the link still holds"), "The timing table still holds.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm link" }));
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ pageVersion: 3, comment: "The timing table still holds." }));
  });

  it("shows a reader without a token the space and the stored version, and how to connect", async () => {
    const sent = stubPage({ account: withoutToken, edit: false });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Stator pages" });
    await waitFor(() => expect(panel.querySelector(`[data-stator-page="${TIMING}"]`)).not.toBeNull());
    const chip = panel.querySelector<HTMLElement>(`[data-stator-page="${TIMING}"]`)!;
    expect(chip).toHaveAttribute("data-state", "connect");
    expect(within(chip).getByRole("link", { name: "ENG Page in Stator" })).toHaveAttribute("href", `${BASE}/s/ENG/p/${TIMING}`);
    expect(chip).toHaveTextContent("Version 2");
    expect(within(chip).getByRole("link", { name: /Connect your Stator account/ })).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/stator/pages")).toBe(false);
    expect(within(panel).queryByRole("button", { name: /Remove/ })).toBeNull();
  });

  it("is not there while Exciter has no Stator", async () => {
    stubPage({ account: { ...withoutToken, configured: false, baseUrl: "", status: "not_configured" } });
    await renderAt("/projects/CHK/r/CHK-R1");
    await screen.findByRole("region", { name: "Trace links" });
    expect(screen.queryByRole("region", { name: "Stator pages" })).toBeNull();
  });

  it("links the requirement to a page found by its title, and says why another was refused", async () => {
    let links = [aLink(TIMING)];
    const sent = stubPage({
      links,
      more: {
        "GET /requirements/CHK-R1/stator-links": () => ({ status: 200, body: { links } }),
        "GET /stator/search": {
          status: 200,
          body: { status: "ok", pages: [{ id: ONBOARDING, title: "Onboarding guide", spaceKey: "ENG", spaceName: "Engineering", path: [], url: "" }] },
        },
        "POST /requirements/CHK-R1/stator-links": () => {
          if (links.length > 1) {
            return {
              status: 409,
              body: { error: { code: "stator_link_exists", message: "This requirement is linked to that page with that relation already." } },
            };
          }
          const made = aLink(ONBOARDING, { relation: "relates", pageVersion: 1, pageVersionSeen: 1 });
          links = [...links, made];
          return { status: 201, body: { link: made } };
        },
      },
    });
    const user = userEvent.setup();
    await renderAt("/projects/CHK/r/CHK-R1");
    const form = await screen.findByRole("form", { name: "Link a Stator page" });
    await user.selectOptions(within(form).getByLabelText("Relation"), "relates");
    await user.type(within(form).getByRole("combobox", { name: "Page" }), "onboard");
    await user.click(await within(form).findByRole("option", { name: /Onboarding guide/ }));
    await user.click(within(form).getByRole("button", { name: "Link the page" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ pageId: ONBOARDING, relation: "relates" }));
    expect(await screen.findByText("Linked to Onboarding guide.")).toBeInTheDocument();

    await user.type(within(form).getByRole("combobox", { name: "Page" }), "onboard");
    await user.click(await within(form).findByRole("option", { name: /Onboarding guide/ }));
    await user.click(within(form).getByRole("button", { name: "Link the page" }));
    expect(await within(form).findByText(/linked to that page with that relation already/)).toBeInTheDocument();
  });

  it("removes a trace", async () => {
    let links = [aLink(TIMING)];
    stubPage({
      more: {
        "GET /requirements/CHK-R1/stator-links": () => ({ status: 200, body: { links } }),
        [`DELETE /stator-links/${aLink(TIMING).id}`]: () => {
          links = [];
          return { status: 204 };
        },
      },
    });
    const user = userEvent.setup();
    await renderAt("/projects/CHK/r/CHK-R1");
    await user.click(await screen.findByRole("button", { name: "Remove Brake timing, Specified by the page" }));
    expect(await screen.findByText(/No page yet/)).toBeInTheDocument();
    expect(await screen.findByText("The link to Brake timing was removed.")).toBeInTheDocument();
  });
});
