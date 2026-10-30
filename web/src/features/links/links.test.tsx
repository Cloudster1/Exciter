import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { groupLinks } from "@/api/links";
import { LINK_TYPES_PATH } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aLink, aLinkType, derives, relates, verifies } from "@/test/links";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const project = aProject();
const sys = { key: "SYS-R2", title: "Keep a ledger" };
const test = { key: "CHK-R3", title: "Pay with a card" };

function stubRequirement({
  edit = true,
  links = [aLink(sys), aLink(test, { type: verifies, direction: "incoming" })],
  more = {},
}: {
  edit?: boolean;
  links?: ReturnType<typeof aLink>[];
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const requirement = aRequirement({ can: { edit, transition: edit, delete: edit, link: edit, comment: true } });
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies, relates] } },
    "GET /search/quick": {
      status: 200,
      body: {
        requirements: [
          { outline: "2", projectName: "Checkout", requirement: { ...aLink({ key: "CHK-R2", title: "Refund a payment" }).other } },
          { outline: "", projectName: "Checkout", requirement: { ...aLink({ key: "CHK-R9", title: "Money" }).other, kind: "section" } },
        ],
      },
    },
    ...more,
  });
}

describe("groupLinks", () => {
  it("gathers links under their type and direction, and a symmetric type under one heading", () => {
    const groups = groupLinks([
      aLink(sys),
      aLink({ key: "SYS-R3", title: "Audit" }),
      aLink(test, { type: derives, direction: "incoming" }),
      aLink({ key: "CHK-R4", title: "A" }, { type: relates }),
      aLink({ key: "CHK-R5", title: "B" }, { type: relates, direction: "incoming" }),
    ]);
    expect(groups.map((g) => `${g.type.key}:${g.direction}:${g.links.length}`)).toEqual(["derives:outgoing:2", "derives:incoming:1", "relates:outgoing:2"]);
  });
});

describe("a requirement's links", () => {
  it("lists them by type and direction, each a way to the other end, and offers a reader nothing to change", async () => {
    stubRequirement({ edit: false });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Trace links" });
    const derived = await within(panel).findByRole("heading", { name: "Derives from" });
    expect(derived.closest("[data-link-group]")).toHaveAttribute("data-link-group", "derives:outgoing");
    expect(within(panel).getByRole("heading", { name: "Is verified by" })).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: "SYS-R2 Keep a ledger" })).toHaveAttribute("href", "/projects/SYS/r/SYS-R2");
    expect(within(panel).queryByRole("button", { name: /Remove the link/ })).not.toBeInTheDocument();
    expect(within(panel).queryByRole("form", { name: "Add a link" })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("links to a requirement found by its key, read the way the type was picked", async () => {
    const sent = stubRequirement({
      links: [],
      more: {
        "POST /requirements/CHK-R1/links": {
          status: 201,
          body: { link: aLink({ key: "CHK-R2", title: "Refund a payment" }, { type: verifies, direction: "incoming" }) },
        },
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    expect(await screen.findByText("Nothing is linked to this requirement yet.")).toBeInTheDocument();
    const form = await screen.findByRole("form", { name: "Add a link" });
    const select = within(form).getByRole("combobox", { name: "Link type" });
    await waitFor(() => expect(within(select).getAllByRole("option")).toHaveLength(5));
    expect(
      within(select)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual([
      "This requirement derives from",
      "This requirement is derived into",
      "This requirement verifies",
      "This requirement is verified by",
      "This requirement relates to",
    ]);
    await userEvent.selectOptions(select, "This requirement is verified by");
    const create = within(form).getByRole("button", { name: "Link" });
    expect(create).toBeDisabled();

    await userEvent.type(within(form).getByRole("combobox", { name: "Requirement" }), "chk-r");
    const options = await screen.findByRole("listbox", { name: "Requirements" });
    await waitFor(() =>
      expect(
        within(options)
          .getAllByRole("option")
          .map((o) => o.getAttribute("data-picker-option")),
      ).toEqual(["CHK-R2"]),
    );
    expect(await axeViolations()).toEqual([]);
    await userEvent.keyboard("{Enter}");
    expect(within(form).getByText("CHK-R2 Refund a payment")).toBeInTheDocument();
    await userEvent.click(create);
    await waitFor(() => expect(sent.find((each) => each.method === "POST")?.body).toEqual({ typeId: verifies.id, direction: "incoming", otherKey: "CHK-R2" }));
    expect(await screen.findByText("Linked: is verified by CHK-R2.")).toBeInTheDocument();
  });

  it("says why a link was refused, beside the form", async () => {
    stubRequirement({
      more: {
        "POST /requirements/CHK-R1/links": {
          status: 422,
          body: {
            error: {
              code: "validation_failed",
              message: "Check the highlighted fields.",
              fields: { otherKey: "CHK-R2 already derives from CHK-R1, so this link would close a circle." },
            },
          },
        },
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    const form = await screen.findByRole("form", { name: "Add a link" });
    await userEvent.type(within(form).getByRole("combobox", { name: "Requirement" }), "CHK-R2");
    await screen.findByRole("option", { name: /Refund a payment/ });
    await userEvent.keyboard("{Enter}");
    await userEvent.click(within(form).getByRole("button", { name: "Link" }));
    expect(await within(form).findByText(/would close a circle/)).toBeInTheDocument();
  });

  it("removes a link from this end", async () => {
    const sent = stubRequirement({ more: { [`DELETE /links/${aLink(sys).id}`]: { status: 204 } } });
    await renderAt("/projects/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Remove the link derives from SYS-R2" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/links/${aLink(sys).id}`)).toBe(true));
    expect(await screen.findByText("Removed the link derives from SYS-R2.")).toBeInTheDocument();
  });
});

describe("link types", () => {
  const own = aLinkType({
    id: "0195f000-0000-7000-8000-0000000001b1",
    key: "tests",
    name: "Tests",
    forward: "tests",
    backward: "is tested by",
    scope: "project",
    projectKey: "CHK",
  });

  it("turns a type off in a project and limits another to pairs of requirement types", async () => {
    const requirementType = { id: "0195f000-0000-7000-8000-0000000003a1", name: "Test", retired: false };
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies, own] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [requirementType] } },
      [`PUT /projects/CHK/link-types/${derives.id}`]: { status: 200, body: { linkType: { ...derives, enabled: false } } },
      [`PUT /projects/CHK/link-types/${verifies.id}`]: { status: 200, body: { linkType: verifies } },
    });
    await renderAt("/projects/CHK/settings/links");
    const table = await screen.findByRole("table", { name: "Link types" });
    expect(
      within(table)
        .getAllByRole("row")
        .map((row) => row.getAttribute("data-link-type")),
    ).toEqual([null, "derives", "verifies", "tests"]);
    expect(within(table).getAllByRole("button", { name: "Edit" })).toHaveLength(1);
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(table).getByRole("checkbox", { name: "Use Derives in this project" }));
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({ enabled: false, rules: [] }));

    const row = table.querySelector('[data-link-type="verifies"]') as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Pairs" }));
    const dialog = await screen.findByRole("dialog", { name: "Requirement types Verifies joins" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Add a pair" }));
    await userEvent.selectOptions(within(dialog).getByRole("combobox", { name: "From 1" }), "Test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save pairs" }));
    await waitFor(() =>
      expect(sent.filter((each) => each.method === "PUT").at(-1)?.body).toEqual({
        enabled: true,
        rules: [{ sourceTypeId: requirementType.id, targetTypeId: null }],
      }),
    );
  });

  it("lets the administrators make a type for all projects, and tells anybody else whose it is", async () => {
    const sent = stubApi({
      "GET /link-types": { status: 200, body: { linkTypes: [derives, relates] } },
      "POST /link-types": { status: 201, body: { linkType: aLinkType({ key: "mitigates", name: "Mitigates" }) } },
    });
    await renderAt(LINK_TYPES_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "New link type" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "Mitigates");
    await userEvent.type(screen.getByRole("textbox", { name: "Reads from the source" }), "mitigates");
    await userEvent.type(screen.getByRole("textbox", { name: "Reads from the target" }), "is mitigated by");
    await userEvent.click(screen.getByRole("checkbox", { name: "Refuse links that go round in a circle" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save link type" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toEqual({
        name: "Mitigates",
        forward: "mitigates",
        backward: "is mitigated by",
        acyclic: true,
        suspectEnd: "none",
        counts: "nothing",
      }),
    );
  });

  it("refuses the page for all projects to a member", async () => {
    stubApi({ "GET /link-types": { status: 200, body: { linkTypes: [derives] } } });
    await renderAt(LINK_TYPES_PATH, { me: { ...signedIn, administers: false } });
    expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
  });
});
