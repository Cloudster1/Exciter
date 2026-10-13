import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { normalizeLabel, type LabelledRequirement } from "@/api/labels";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const project = aProject();

function stubRequirement({
  edit = true,
  labels = ["checkout"],
  more = {},
}: {
  edit?: boolean;
  labels?: string[];
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const requirement = aRequirement({ labels, can: { edit, transition: edit, delete: edit, link: edit, comment: true } });
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /labels": (request) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      const known = [
        { name: "release", requirements: 4 },
        { name: "release-2", requirements: 2 },
        { name: "checkout", requirements: 9 },
      ];
      return { status: 200, body: { labels: known.filter((each) => each.name.startsWith(q)) } };
    },
    ...more,
  });
}

const labelled = (key: string, over: Partial<LabelledRequirement> = {}): LabelledRequirement => ({
  id: `0195f000-0000-7000-8000-0000000000${key.length.toString(16).padStart(2, "0")}`,
  key,
  projectKey: key.split("-")[0]!,
  projectName: "Checkout",
  kind: "requirement",
  title: "Pay the basket",
  labels: ["release"],
  updatedByName: "Ada Lovelace",
  updatedAt: "2026-10-01T08:00:00Z",
  ...over,
});

describe("normalizeLabel", () => {
  it("reads a name the way the API stores it", () => {
    expect(normalizeLabel("  Safety Critical ")).toEqual({ name: "safety-critical" });
    expect(normalizeLabel("Übersicht")).toEqual({ name: "übersicht" });
    expect(normalizeLabel("v1.2_b-c")).toEqual({ name: "v1.2_b-c" });
    expect(normalizeLabel("a/b").problem).toBe("invalid");
    expect(normalizeLabel("-lead").problem).toBe("invalid");
    expect(normalizeLabel("").problem).toBe("invalid");
    expect(normalizeLabel("a".repeat(41)).problem).toBe("tooLong");
  });
});

describe("labels on a requirement", () => {
  it("shows each label as a way to the project's requirements that carry it", async () => {
    stubRequirement({ edit: false, labels: ["checkout", "release"] });
    await renderAt("/p/CHK/r/CHK-R1");
    const list = await screen.findByRole("list", { name: "Labels" });
    expect(within(list).getByRole("link", { name: "release" })).toHaveAttribute("href", "/p/CHK?label=release");
    expect(screen.queryByRole("combobox", { name: "Add a label" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove label release" })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("adds a label picked with the arrow keys, and one typed new with Enter", async () => {
    const added: string[] = [];
    const sent = stubRequirement({
      more: {
        "POST /requirements/CHK-R1/labels": async (request) => {
          const { name } = (await request.clone().json()) as { name: string };
          added.push(name);
          return { status: 200, body: { labels: ["checkout", ...added].sort() } };
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const input = await screen.findByRole("combobox", { name: "Add a label" });
    await userEvent.type(input, "rel");
    const options = await screen.findByRole("listbox", { name: "Labels to pick" });
    await waitFor(() =>
      expect(
        within(options)
          .getAllByRole("option")
          .map((o) => o.getAttribute("data-label-option")),
      ).toEqual(["rel", "release", "release-2"]),
    );
    expect(within(options).getByRole("option", { name: /Add rel/ })).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    const active = within(options).getByRole("option", { name: /release-2/ });
    expect(input).toHaveAttribute("aria-activedescendant", active.id);
    expect(await axeViolations()).toEqual([]);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(added).toEqual(["release-2"]));
    expect(input).toHaveValue("");
    expect(await screen.findByRole("link", { name: "release-2" })).toBeInTheDocument();

    await userEvent.type(input, "Safety Critical{Enter}");
    await waitFor(() => expect(added).toEqual(["release-2", "safety-critical"]));
    expect(sent.filter((each) => each.method === "POST" && each.path.endsWith("/labels")).map((each) => each.body)).toEqual([
      { name: "release-2" },
      { name: "safety-critical" },
    ]);
    expect(await screen.findByText("Added the label safety-critical.")).toBeInTheDocument();
  });

  it("says why a typed name cannot be a label, and does not send it", async () => {
    const sent = stubRequirement();
    await renderAt("/p/CHK/r/CHK-R1");
    const input = await screen.findByRole("combobox", { name: "Add a label" });
    await userEvent.type(input, "a/b{Enter}");
    expect(await screen.findByText(/Use letters, digits, hyphens/)).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(sent.some((each) => each.method === "POST" && each.path.endsWith("/labels"))).toBe(false);
  });

  it("takes a label off with its button, and shows a refusal", async () => {
    const removed: string[] = [];
    stubRequirement({
      labels: ["checkout", "release"],
      more: {
        "DELETE /requirements/CHK-R1/labels/release": () => {
          removed.push("release");
          return { status: 204 };
        },
        "POST /requirements/CHK-R1/labels": {
          status: 409,
          body: {
            error: { code: "requirement_trashed", message: "This requirement is in the trash. Restore it from the project's trash to change it." },
          },
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Remove label release" }));
    await waitFor(() => expect(removed).toEqual(["release"]));
    await waitFor(() => expect(screen.queryByRole("link", { name: "release" })).not.toBeInTheDocument());

    await userEvent.type(screen.getByRole("combobox", { name: "Add a label" }), "new{Enter}");
    expect(await screen.findByText(/This requirement is in the trash/)).toBeInTheDocument();
  });
});

describe("the project's list by label", () => {
  it("narrows the list to a label and offers every project and the whole list", async () => {
    const asked: (string | null)[] = [];
    const row = { ...aRequirement({ labels: ["checkout", "release"] }), outline: "", trashed: false };
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects/CHK/requirements": (request) => {
        asked.push(new URL(request.url).searchParams.get("label"));
        return { status: 200, body: { rows: [row], total: 1, limit: 50, offset: 0 } };
      },
    });
    await renderAt("/p/CHK?label=release");
    const filter = await screen.findByText("Showing requirements labelled release");
    expect(asked).toEqual(["release"]);
    expect(within(filter.closest("[data-label-filter]") as HTMLElement).getByRole("link", { name: "Show every project" })).toHaveAttribute(
      "href",
      "/labels/release",
    );
    const labels = await screen.findByRole("list", { name: "Labels" });
    expect(within(labels).getByRole("link", { name: "checkout" })).toHaveAttribute("href", "/p/CHK?label=checkout");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the label page", () => {
  it("lists the requirements with a label in every project, a page of the list at a time", async () => {
    const offsets: string[] = [];
    stubApi({
      "GET /labels/release/requirements": (request) => {
        const params = new URL(request.url).searchParams;
        offsets.push(params.get("offset") ?? "0");
        expect(params.get("project")).toBeNull();
        return {
          status: 200,
          body: {
            requirements: [labelled("CHK-R7", { title: "Refund the basket", labels: ["plan", "release"] })],
            total: 45,
            limit: 20,
            offset: Number(params.get("offset") ?? 0),
          },
        };
      },
    });
    const router = await renderAt("/labels/release");
    expect(await screen.findByText("1 to 1 of 45 requirements")).toBeInTheDocument();
    const row = document.querySelector('[data-labelled-requirement="CHK-R7"]') as HTMLElement;
    expect(within(row).getByRole("link", { name: "Refund the basket" })).toHaveAttribute("href", "/p/CHK/r/CHK-R7");
    expect(row).toHaveTextContent("Checkout / Ada Lovelace changed it on");
    expect(within(row).getByRole("link", { name: "plan" })).toHaveAttribute("href", "/labels/plan");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ page: 2 }));
    await waitFor(() => expect(offsets.at(-1)).toBe("20"));
  });

  it("narrows to one project, offers every project, and says when nothing carries the label", async () => {
    stubApi({
      "GET /labels/lonely/requirements": (request) => {
        expect(new URL(request.url).searchParams.get("project")).toBe("CHK");
        return { status: 200, body: { requirements: [], total: 0, limit: 20, offset: 0 } };
      },
    });
    await renderAt("/labels/lonely?project=CHK");
    expect(await screen.findByText("No requirement carries this label")).toBeInTheDocument();
    expect(screen.getByText("Requirements in CHK with this label")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Show every project" })).toHaveAttribute("href", "/labels/lonely");
    expect(await axeViolations()).toEqual([]);
  });
});
