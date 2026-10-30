import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QualityFinding, QualityFindings, QualityRule, QualitySettings, QualitySummary } from "@/api/quality";
import type { RuleType } from "@/api/workflows";
import { RuleEditor } from "@/features/workflows/designer/RuleEditor";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// Writing rules (#207): findings marked as one writes and when reading, accepted with a reason, a project's scores,
// its rules, and the workflow rule asking for a score.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const statement = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "The page shall load quickly." }] }] };
const requirement = aRequirement({ statement, quality: { score: 93, active: 1, accepted: 0, stale: false } });

function aFinding(over: Partial<QualityFinding> = {}): QualityFinding {
  return {
    key: "vague:statement:quickly:0",
    rule: "vague",
    severity: "warning",
    field: "statement",
    block: 0,
    from: 20,
    to: 27,
    match: "quickly",
    message: "quickly is vague.",
    fix: "Say how fast.",
    active: true,
    accepted: null,
    ...over,
  };
}

function findings(over: Partial<QualityFindings> = {}): QualityFindings {
  return {
    revision: 1,
    score: 93,
    active: 1,
    accepted: 0,
    stale: false,
    findings: [aFinding()],
    sentences: [{ field: "statement", block: 0, from: 0, to: 28, pattern: "ubiquitous" }],
    ...over,
  };
}

describe("a requirement's findings", () => {
  it("are marked in its text on request, listed with how to fix them, and accepted with a reason", async () => {
    const accepted = aFinding({
      active: false,
      accepted: {
        reason: "Load time is set by the hosting contract.",
        by: { id: "0195f000-0000-7000-8000-00000000000a", name: "Ada Lovelace" },
        revision: 1,
        carriedFrom: null,
        at: "2026-10-18T08:00:00Z",
      },
    });
    const afterwards = findings({ score: 100, active: 0, accepted: 1, findings: [accepted] });
    let given = false;
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/quality": () => ({ status: 200, body: { quality: given ? afterwards : findings() } }),
      "POST /requirements/CHK-R1/quality/accepted": () => {
        given = true;
        return { status: 200, body: { quality: afterwards } };
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    expect(await screen.findByText("Quality 93")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Show findings" }));
    const list = await screen.findByRole("region", { name: "Writing rules" });
    expect(document.querySelector('[data-statement] [data-finding="vague:statement:quickly:0"]')?.textContent).toBe("quickly");
    expect(within(list).getByRole("status")).toHaveTextContent("1 finding.");
    expect(within(list).getByText("Vague word")).toBeInTheDocument();
    expect(within(list).getByText(/a time, a number or a named behaviour/)).toBeInTheDocument();
    expect(within(list).getByText("Sentence 1: Ubiquitous")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(list).getByRole("button", { name: "Accept: quickly" }));
    const dialog = await screen.findByRole("dialog", { name: "Accept the finding" });
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Reason" }), "Load time is set by the hosting contract.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Accept" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST" && r.path.endsWith("/quality/accepted"))?.body).toEqual({
        revision: 1,
        key: "vague:statement:quickly:0",
        reason: "Load time is set by the hosting contract.",
      }),
    );
    expect(await within(list).findByText("Accepted by Ada Lovelace: Load time is set by the hosting contract.")).toBeInTheDocument();
    expect(within(list).getByRole("button", { name: "Take back: quickly" })).toBeInTheDocument();
  });

  it("are marked in the statement editor as the draft is checked", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/quality": { status: 200, body: { quality: findings() } },
      "POST /projects/CHK/quality-check": {
        status: 200,
        body: { check: { score: 93, language: "en", findings: [aFinding()], sentences: [], terms: [] } },
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const mark = await waitFor(
      () => {
        const found = document.querySelector('#edit-statement [data-finding="vague:statement:quickly:0"]');
        expect(found).not.toBeNull();
        return found!;
      },
      { timeout: 3000 },
    );
    expect(mark.textContent).toBe("quickly");
    expect(mark).toHaveClass("finding", "finding-warning");
    expect(sent.find((r) => r.method === "POST" && r.path === "/projects/CHK/quality-check")?.body).toEqual({
      title: "Pay the basket",
      statement,
      requirementKey: "CHK-R1",
    });
    expect(await screen.findByRole("button", { name: "Show in text: quickly" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Accept: quickly" })).toBeInTheDocument();
  });
});

describe("the quality page", () => {
  const summary: QualitySummary = {
    checked: 3,
    unchecked: 1,
    average: 71,
    bands: [
      { from: 0, to: 19, count: 0 },
      { from: 20, to: 39, count: 1 },
      { from: 40, to: 59, count: 0 },
      { from: 60, to: 79, count: 1 },
      { from: 80, to: 100, count: 1 },
    ],
    rules: [
      { rule: "passive", count: 2 },
      { rule: "vague", count: 1 },
    ],
    requirements: [
      {
        id: "0195f000-0000-7000-8000-0000000000a2",
        key: "CHK-R2",
        title: "Encrypt the card",
        score: 31,
        active: 4,
        accepted: 1,
        stale: false,
        rules: ["passive", "vague"],
      },
    ],
    total: 1,
    limit: 50,
    offset: 0,
  };

  it("shows the bands, the rules broken most and the requirements lowest first, narrowed by a query", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/quality": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { quality: summary } };
      },
    });
    await renderAt("/projects/CHK/quality");
    const table = await screen.findByRole("table", { name: "Lowest first" });
    expect(within(table).getByRole("link", { name: /CHK-R2/ })).toHaveAttribute("href", "/projects/CHK/r/CHK-R2");
    expect(within(table).getByText("Quality 31")).toBeInTheDocument();
    expect(within(table).getByText("4 open, 1 accepted")).toBeInTheDocument();
    expect(within(screen.getByRole("list", { name: "Requirements by score" })).getByText("20 to 39: 1")).toBeInTheDocument();
    const broken = screen.getByRole("list", { name: "Rules broken most" });
    expect(within(broken).getByRole("link", { name: "Passive voice" }).getAttribute("href")).toContain("qualityFinding");
    expect(await axeViolations()).toEqual([]);
    const box = screen.getByRole("combobox", { name: "Narrow with a query" });
    await userEvent.type(box, "quality < 50{Enter}");
    await waitFor(() => expect(asked.at(-1)?.get("query")).toBe("quality < 50"));
  });
});

describe("the writing rules of a project", () => {
  const rules: QualityRule[] = (["vague", "passive"] as const).map((id) => ({
    id,
    defaultSeverity: "warning",
    defaultEnabled: true,
    fields: ["title", "statement"],
    words: id === "vague",
    label: id,
    description: id,
  }));
  const settings: QualitySettings = {
    language: "en",
    updatedAt: null,
    rules: [
      { rule: "vague", enabled: true, severity: "warning", typeIds: [] },
      { rule: "passive", enabled: true, severity: "warning", typeIds: [] },
    ],
    words: { vague: { add: ["snappy"], except: [] } },
  };
  const functional = { id: "0195f000-0000-7000-8000-0000000000c1", name: "Functional", retired: false };

  it("are switched, weighed and given types by an administrator", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/types": { status: 200, body: { types: [functional] } },
      "GET /projects/CHK/quality-settings": { status: 200, body: { settings } },
      "GET /quality-rules": { status: 200, body: { rules } },
      "PUT /projects/CHK/quality-settings": { status: 200, body: { settings: { ...settings, updatedAt: "2026-10-18T08:00:00Z" } } },
    });
    await renderAt("/projects/CHK/settings/quality");
    const card = await screen.findByRole("form", { name: "Writing rules" });
    await userEvent.click(within(card).getByRole("checkbox", { name: "Check Passive voice" }));
    await userEvent.selectOptions(within(card).getByRole("combobox", { name: "Severity of Vague word" }), "error");
    const types = within(card).getByRole("group", { name: "Types Vague word applies to" });
    await userEvent.click(within(types).getByRole("checkbox", { name: "Functional" }));
    expect(screen.getByRole("textbox", { name: "Vague word: words to add" })).toHaveValue("snappy");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(card).getByRole("button", { name: "Save rules" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")).toBeDefined());
    const body = sent.find((r) => r.method === "PUT")!.body as { rules: unknown[]; words: Record<string, unknown> };
    expect(body.rules).toContainEqual({ rule: "vague", enabled: true, severity: "error", typeIds: [functional.id] });
    expect(body.rules).toContainEqual({ rule: "passive", enabled: false, severity: "warning", typeIds: [] });
    expect(body.words.vague).toEqual({ add: ["snappy"], except: [] });
    // The saved settings start the form again, and the notice outlives it.
    expect(await screen.findByText("Saved. The scores follow.")).toBeInTheDocument();
  });

  it("take the words an administrator adds and the ones removed, every list at once, with the rules as they are", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/quality-settings": { status: 200, body: { settings } },
      "GET /quality-rules": { status: 200, body: { rules } },
      "PUT /projects/CHK/quality-settings": { status: 200, body: { settings: { ...settings, updatedAt: "2026-10-18T08:00:00Z" } } },
    });
    await renderAt("/projects/CHK/settings/quality");
    const card = await screen.findByRole("form", { name: "Word lists" });
    const vague = within(card).getByRole("textbox", { name: "Vague word: words to add" });
    expect(vague).toHaveValue("snappy");
    await userEvent.clear(vague);
    await userEvent.type(vague, "zippy, , brisk ");
    await userEvent.type(within(card).getByRole("textbox", { name: "Vague word: built-in words not to find" }), "quickly");
    await userEvent.type(within(card).getByRole("textbox", { name: "Unbounded word: words to add" }), "every");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(card).getByRole("button", { name: "Save word lists" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")).toBeDefined());
    const body = sent.find((r) => r.method === "PUT")!.body as { rules: unknown[]; words: Record<string, unknown> };
    expect(body.words).toEqual({
      vague: { add: ["zippy", "brisk"], except: ["quickly"] },
      unbounded: { add: ["every"], except: [] },
      escape: { add: [], except: [] },
      placeholder: { add: [], except: [] },
    });
    expect(body.rules).toContainEqual({ rule: "vague", enabled: true, severity: "warning", typeIds: [] });
    expect(body.rules).toContainEqual({ rule: "passive", enabled: true, severity: "warning", typeIds: [] });
    expect(await screen.findByText("Saved. The requirements are being checked again.")).toBeInTheDocument();
  });

  it("are read but not changed by somebody who does not administer the project", async () => {
    stubApi({
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: true, edit: true, approve: true, administer: false, purge: false } }) },
      },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/quality-settings": { status: 200, body: { settings } },
      "GET /quality-rules": { status: 200, body: { rules } },
    });
    await renderAt("/projects/CHK/settings/quality");
    const card = await screen.findByRole("form", { name: "Writing rules" });
    expect(within(card).getByRole("checkbox", { name: "Check Vague word" })).toBeDisabled();
    expect(within(card).queryByRole("button", { name: "Save rules" })).toBeNull();
  });
});

describe("the workflow rule asking for a score", () => {
  it("takes a whole number within its bounds", async () => {
    const catalogue: RuleType[] = [
      {
        kind: "validator",
        type: "validator.quality_min",
        label: "Quality score",
        description: "",
        options: [{ name: "minimum", label: "Least score", kind: "number", min: 0, max: 100, required: true, choices: [] }],
      },
    ];
    const onChange = vi.fn();
    render(<RuleEditor rules={[]} catalogue={catalogue} onChange={onChange} />);
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Add a rule" }), "validator.quality_min");
    const minimum = screen.getByRole("spinbutton", { name: "Least score" });
    expect(minimum).toHaveAttribute("min", "0");
    expect(minimum).toHaveAttribute("max", "100");
    const add = screen.getByRole("button", { name: "Add rule" });
    await userEvent.type(minimum, "170");
    expect(add).toBeDisabled();
    await userEvent.clear(minimum);
    await userEvent.type(minimum, "70");
    await userEvent.click(add);
    expect(onChange).toHaveBeenCalledWith([{ kind: "validator", type: "validator.quality_min", config: { minimum: 70 } }]);
  });
});
