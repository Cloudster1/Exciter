import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Editor as TiptapEditor } from "@tiptap/core";
import type { Editor as ReactEditor } from "@tiptap/react";
import type { SpecDocument } from "@/api/documents";
import type { Glossaries, RequirementTerm, Term, TermRevision, TermUse } from "@/api/glossary";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { editorExtensions } from "@/features/editor/extensions";
import { TermPickerDialog } from "./TermPicker";

// The glossary (#210): its pages, a requirement's terms and a linked word read as a button, the print view's chapter,
// the library glossaries a project takes, and terms the search finds.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("data-print-ready");
});

const TERM_ID = "0195f000-0000-7000-8000-0000000007a1";
const LIBRARY_TERM_ID = "0195f000-0000-7000-8000-0000000007a2";
const ada = { id: "0195f000-0000-7000-8000-00000000000a", name: "Ada Lovelace" };
const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

function aTerm(over: Partial<Term> = {}): Term {
  return {
    id: TERM_ID,
    projectKey: "CHK",
    name: "Brake pedal",
    status: "approved",
    definition: paragraph("The lever the driver presses to slow the car."),
    synonyms: ["brake"],
    avoid: ["stopper"],
    revision: 2,
    definitionRevision: 2,
    uses: 1,
    createdBy: ada,
    updatedBy: ada,
    createdAt: "2026-10-01T08:00:00Z",
    updatedAt: "2026-10-10T08:00:00Z",
    ...over,
  };
}

const ref = { id: TERM_ID, projectKey: "CHK", name: "Brake pedal", status: "approved" as const };

describe("the glossary page", () => {
  it("lists the project's terms and its libraries', finds them by their words, and takes a new term", async () => {
    const asked: URLSearchParams[] = [];
    const library = aTerm({ id: LIBRARY_TERM_ID, projectKey: "LIB", name: "Wheel", synonyms: [], uses: 0 });
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/terms": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { terms: [aTerm(), library], total: 2, limit: 50, offset: 0 } };
      },
      "POST /projects/CHK/terms": { status: 201, body: { term: aTerm({ id: "0195f000-0000-7000-8000-0000000007a3", name: "Throttle" }) } },
    });
    await renderAt("/p/CHK/glossary");
    const table = await screen.findByRole("table", { name: "Glossary" });
    expect(within(table).getByRole("link", { name: "Brake pedal" })).toHaveAttribute("href", `/p/CHK/glossary/${TERM_ID}`);
    expect(within(table).getByRole("link", { name: "Wheel" })).toHaveAttribute("href", `/p/LIB/glossary/${LIBRARY_TERM_ID}`);
    const row = document.querySelector('[data-term-row="Brake pedal"]') as HTMLElement;
    expect(row).toHaveTextContent("This project");
    expect(row).toHaveTextContent("1 requirement");
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(screen.getByRole("searchbox", { name: "Search terms" }), "pedal");
    await waitFor(() => expect(asked.at(-1)?.get("q")).toBe("pedal"));
    await userEvent.click(screen.getByRole("checkbox", { name: /Only this project's terms/ }));
    await waitFor(() => expect(asked.at(-1)?.get("own")).toBe("true"));

    await userEvent.click(screen.getByRole("button", { name: "New term" }));
    const form = await screen.findByRole("region", { name: "A new term" });
    await userEvent.type(within(form).getByRole("textbox", { name: "Name" }), "Throttle");
    await userEvent.type(within(form).getByRole("textbox", { name: "Synonyms" }), "gas pedal{Enter}");
    await userEvent.type(within(form).getByRole("textbox", { name: "Words to avoid" }), "accelerator{Enter}");
    expect(within(form).getByRole("button", { name: "Remove gas pedal" })).toBeInTheDocument();
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Status" }), "approved");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(form).getByRole("button", { name: "Add term" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")).toBeDefined());
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({
      name: "Throttle",
      definition: { type: "doc", content: [{ type: "paragraph" }] },
      synonyms: ["gas pedal"],
      avoid: ["accelerator"],
      status: "approved",
    });
    expect(await screen.findByText("Throttle is in the glossary.")).toBeInTheDocument();
  });
});

describe("a term's page", () => {
  const revisions: TermRevision[] = [
    {
      number: 2,
      name: "Brake pedal",
      definition: paragraph("The lever the driver presses to slow the car."),
      synonyms: ["brake"],
      avoid: ["stopper"],
      status: "approved",
      comment: "Say who presses it.",
      createdBy: ada,
      createdAt: "2026-10-10T08:00:00Z",
    },
    {
      number: 1,
      name: "Brake",
      definition: paragraph("The lever to slow the car."),
      synonyms: [],
      avoid: [],
      status: "proposed",
      comment: "",
      createdBy: ada,
      createdAt: "2026-10-01T08:00:00Z",
    },
  ];
  const use: TermUse = {
    requirement: {
      id: "0195f000-0000-7000-8000-0000000000a1",
      key: "CHK-R1",
      projectKey: "CHK",
      kind: "requirement",
      title: "Stop the car",
      revision: 1,
      status: null,
      trashed: false,
    },
    explicit: false,
    seenRevision: 1,
    suspect: true,
  };

  it("shows the definition, what each revision changed and the requirements that use it, and is changed over its revision", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      [`GET /terms/${TERM_ID}`]: { status: 200, body: { term: aTerm() } },
      [`GET /terms/${TERM_ID}/revisions`]: { status: 200, body: { revisions } },
      [`GET /terms/${TERM_ID}/uses`]: { status: 200, body: { uses: [use], total: 1, limit: 50, offset: 0 } },
      [`PATCH /terms/${TERM_ID}`]: { status: 200, body: { term: aTerm({ revision: 3, status: "deprecated" }) } },
      [`DELETE /terms/${TERM_ID}`]: { status: 204 },
      "GET /projects/CHK/terms": { status: 200, body: { terms: [], total: 0, limit: 50, offset: 0 } },
    });
    const router = await renderAt(`/p/CHK/glossary/${TERM_ID}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Brake pedal" })).toBeInTheDocument();
    expect(screen.getByText("The lever the driver presses to slow the car.")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Synonyms" })).getByText("brake")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Words to avoid" })).getByText("stopper")).toBeInTheDocument();
    const second = await waitFor(() => {
      const found = document.querySelector('[data-term-revision="2"]');
      expect(found).not.toBeNull();
      return found as HTMLElement;
    });
    expect(second.querySelector('[data-term-change="Definition"] ins')).toHaveTextContent("Added: the driver presses");
    expect(second.querySelector('[data-term-change="Name"] ins')).toHaveTextContent("Added: pedal");
    expect(document.querySelector('[data-term-revision="1"]')).toHaveTextContent("The first revision.");
    const uses = screen.getByRole("table", { name: "Used by" });
    expect(within(uses).getByRole("link", { name: /CHK-R1/ })).toHaveAttribute("href", "/p/CHK/r/CHK-R1");
    expect(within(uses).getByText("Suspect")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Status" }), "deprecated");
    await userEvent.type(screen.getByRole("textbox", { name: "Comment on the change" }), "Replaced by Brake lever.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save term" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")).toBeDefined());
    expect(sent.find((r) => r.method === "PATCH")?.body).toMatchObject({
      baseRevision: 2,
      name: "Brake pedal",
      synonyms: ["brake"],
      avoid: ["stopper"],
      status: "deprecated",
      comment: "Replaced by Brake lever.",
    });
    expect(await screen.findByText("Saved as revision 3.")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Delete term" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete the term" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/CHK/glossary"));
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/terms/${TERM_ID}`)).toBe(true);
  });

  it("offers no change to somebody who only reads the project", async () => {
    stubApi({
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: true, edit: false, approve: false, administer: false, purge: false } }) },
      },
      [`GET /terms/${TERM_ID}`]: { status: 200, body: { term: aTerm() } },
      [`GET /terms/${TERM_ID}/revisions`]: { status: 200, body: { revisions } },
      [`GET /terms/${TERM_ID}/uses`]: { status: 200, body: { uses: [], total: 0, limit: 50, offset: 0 } },
    });
    await renderAt(`/p/CHK/glossary/${TERM_ID}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Brake pedal" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete term" })).toBeNull();
    expect(await screen.findByText("No requirement uses this term yet.")).toBeInTheDocument();
  });
});

describe("a requirement's terms", () => {
  const suspect: RequirementTerm = { term: ref, explicit: false, seenRevision: 1, definitionRevision: 2, suspect: true };

  it("are listed with a suspect use, which its editor confirms with a comment", async () => {
    let cleared = false;
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/terms": () => ({ status: 200, body: { terms: [cleared ? { ...suspect, seenRevision: 2, suspect: false } : suspect] } }),
      [`POST /requirements/CHK-R1/terms/${TERM_ID}/clear`]: () => {
        cleared = true;
        return { status: 200, body: { term: { ...suspect, seenRevision: 2, suspect: false } } };
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "Terms" });
    const item = await within(panel).findByText("Brake pedal");
    expect(item.closest("a")).toHaveAttribute("href", `/p/CHK/glossary/${TERM_ID}`);
    expect(within(panel).getByText("By its words")).toBeInTheDocument();
    expect(within(panel).getByText("Suspect")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(panel).getByRole("button", { name: "Confirm the requirement under the new definition of Brake pedal" }));
    const dialog = await screen.findByRole("dialog", { name: "Confirm the requirement" });
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Comment" }), "The driver still presses it.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST" && r.path.endsWith("/clear"))?.body).toEqual({ termRevision: 2, comment: "The driver still presses it." }),
    );
    expect(await within(panel).findByText("Confirmed under the definition of Brake pedal.")).toBeInTheDocument();
    await waitFor(() => expect(within(panel).queryByText("Suspect")).toBeNull());
  });

  it("read as a button where the words are linked to a term, which shows its definition", async () => {
    const statement = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "The " },
            { type: "text", text: "stop lever", marks: [{ type: "term", attrs: { termId: TERM_ID } }] },
            { type: "text", text: " shall return." },
          ],
        },
      ],
    };
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement({ statement }) } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/terms": { status: 200, body: { terms: [{ ...suspect, explicit: true, seenRevision: 2, suspect: false }] } },
      [`GET /terms/${TERM_ID}`]: { status: 200, body: { term: aTerm() } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const words = await screen.findByRole("button", { name: "stop lever" });
    expect(words).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(words);
    const definition = await screen.findByRole("dialog", { name: "Brake pedal" });
    expect(within(definition).getByText("The lever the driver presses to slow the car.")).toBeInTheDocument();
    expect(within(definition).getByRole("link", { name: "Open in the glossary" })).toHaveAttribute("href", `/p/CHK/glossary/${TERM_ID}`);
    expect(words).toHaveAttribute("aria-expanded", "true");
    expect(await axeViolations()).toEqual([]);
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Brake pedal" })).toBeNull());
  });
});

describe("terms while a requirement is written", () => {
  it("are marked in the statement editor as the draft is checked, with a way to link words to a term", async () => {
    const statement = paragraph("The brake pedals shall return.");
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement({ statement }) } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/terms": { status: 200, body: { terms: [] } },
      "POST /projects/CHK/quality-check": {
        status: 200,
        body: {
          check: {
            score: 100,
            language: "en",
            findings: [],
            sentences: [],
            terms: [
              {
                termId: TERM_ID,
                name: "Brake pedal",
                projectKey: "CHK",
                field: "statement",
                block: 0,
                from: 4,
                to: 16,
                explicit: false,
                definitionPreview: "The lever the driver presses to slow the car.",
              },
            ],
          },
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const mark = await waitFor(
      () => {
        const found = document.querySelector(`#edit-statement .term-use[data-term-use="${TERM_ID}"]`);
        expect(found).not.toBeNull();
        return found!;
      },
      { timeout: 3000 },
    );
    expect(mark.textContent).toBe("brake pedals");
    expect(screen.getByRole("button", { name: "Link to term" })).toBeInTheDocument();
  });

  it("are linked on purpose to the words selected, from the project's terms", async () => {
    stubApi({ "GET /projects/CHK/terms": { status: 200, body: { terms: [aTerm()], total: 1, limit: 20, offset: 0 } } });
    const editor = new TiptapEditor({
      element: document.createElement("div"),
      extensions: editorExtensions(),
      content: paragraph("The stop lever shall return."),
    });
    editor.commands.setTextSelection({ from: 5, to: 15 });
    const closed = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TermPickerDialog projectKey="CHK" editor={editor as unknown as ReactEditor} onClose={closed} />
      </QueryClientProvider>,
    );
    const dialog = await screen.findByRole("dialog", { name: "Link the words to a term" });
    expect(within(dialog).getByText('The words "stop lever" are linked to the term you pick.')).toBeInTheDocument();
    const box = within(dialog).getByRole("combobox", { name: "Find a term" });
    expect(await within(dialog).findByRole("option", { name: "Brake pedal" })).toBeInTheDocument();
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await userEvent.type(box, "{Enter}");
    expect(closed).toHaveBeenCalled();
    expect(JSON.stringify(editor.getJSON())).toContain(`{"type":"text","marks":[{"type":"term","attrs":{"termId":"${TERM_ID}"}}],"text":"stop lever"}`);
    editor.destroy();
  });
});

describe("the print view's glossary", () => {
  it("is a chapter after the entries, and words linked to a term it holds lead to its entry", async () => {
    const document_: SpecDocument = {
      kind: "project",
      title: "Checkout",
      projectKey: "CHK",
      projectName: "Checkout",
      sectionKey: "",
      baseline: null,
      exportedAt: "2026-10-15T10:00:00Z",
      exportedBy: "Ada Lovelace",
      total: 1,
      entries: [
        {
          id: "0195f000-0000-7000-8000-0000000008b2",
          key: "CHK-R1",
          kind: "requirement",
          outline: "1",
          depth: 0,
          title: "Stop the car",
          revision: 1,
          statement: {
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [
                  { type: "text", text: "The " },
                  { type: "text", text: "stop lever", marks: [{ type: "term", attrs: { termId: TERM_ID } }] },
                  { type: "text", text: " and the " },
                  { type: "text", text: "old word", marks: [{ type: "term", attrs: { termId: LIBRARY_TERM_ID } }] },
                  { type: "text", text: " shall return." },
                ],
              },
            ],
          },
          rationale: null,
          properties: [],
          links: [],
        },
      ],
      glossary: [
        {
          id: TERM_ID,
          name: "Brake pedal",
          definition: paragraph("The lever the driver presses to slow the car."),
          synonyms: ["brake"],
          avoid: ["stopper"],
          status: "approved",
          projectKey: "LIB",
          projectName: "Vehicle words",
        },
      ],
    };
    stubApi({ "GET /projects/CHK/document": { status: 200, body: { document: document_ } } });
    await renderAt("/print/p/CHK/document", { ready: "[data-print-view], [data-print-failed]" });
    const chapter = await screen.findByRole("heading", { level: 2, name: "Glossary" });
    expect(within(screen.getByRole("navigation", { name: "Contents" })).getByRole("link", { name: "Glossary" })).toHaveAttribute("href", "#glossary");
    expect(screen.getByRole("link", { name: "stop lever" })).toHaveAttribute("href", `#term-${TERM_ID}`);
    expect(screen.queryByRole("link", { name: "old word" })).toBeNull();
    expect(document.querySelector('[data-print-entry="CHK-R1"]')).toHaveTextContent("The stop lever and the old word shall return.");
    const entry = chapter.closest("[data-print-glossary]") as HTMLElement;
    expect(entry.querySelector(`#term-${TERM_ID}`)).toHaveTextContent("Brake pedal");
    expect(within(entry).getByText("The lever the driver presses to slow the car.")).toBeInTheDocument();
    expect(within(entry).getByText("Synonyms: brake")).toBeInTheDocument();
    expect(within(entry).getByText("Not: stopper")).toBeInTheDocument();
    expect(within(entry).getByText("From Vehicle words (LIB)")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the library glossaries of a project", () => {
  const glossaries: Glossaries = { libraries: [{ projectKey: "LIB", name: "Vehicle words", position: 0, terms: 12 }], updatedAt: "2026-10-10T08:00:00Z" };
  const library = (key: string, name: string) =>
    aProject({ id: `0195f000-0000-7000-8000-0000000000${key === "LIB" ? "e1" : "e2"}`, key, name, settings: { ...aProject().settings, library: true } });

  it("are chosen in order by an administrator from the library projects", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects": { status: 200, body: { projects: [aProject(), library("LIB", "Vehicle words"), library("STD", "Standards")] } },
      "GET /projects/CHK/glossaries": { status: 200, body: { glossaries } },
      "PUT /projects/CHK/glossaries": { status: 200, body: { glossaries: { ...glossaries, updatedAt: "2026-10-18T08:00:00Z" } } },
      "GET /projects/CHK/quality-settings": { status: 200, body: { settings: { language: "en", updatedAt: null, rules: [], words: {} } } },
      "GET /quality-rules": { status: 200, body: { rules: [] } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    });
    await renderAt("/p/CHK/settings?tab=quality");
    const card = await screen.findByRole("region", { name: "Library glossaries" });
    const chosen = await within(card).findByRole("list", { name: "Chosen libraries" });
    expect(within(chosen).getByText("Vehicle words")).toBeInTheDocument();
    expect(within(chosen).getByText("12 terms")).toBeInTheDocument();
    const add = await within(card).findByRole("combobox", { name: "Add a library" });
    expect(within(add).queryByRole("option", { name: /Checkout/ })).toBeNull();
    await userEvent.selectOptions(add, "STD");
    await userEvent.click(within(card).getByRole("button", { name: "Add" }));
    await userEvent.click(within(card).getByRole("button", { name: "Move Standards up" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(card).getByRole("button", { name: "Save libraries" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ libraries: ["STD", "LIB"] }));
    expect(await within(card).findByText("The library glossaries are saved.")).toBeInTheDocument();
  });
});

describe("the search", () => {
  it("shows the glossary terms its words find above the requirements", async () => {
    stubApi({
      "GET /search": {
        status: 200,
        body: {
          hits: [],
          terms: [{ id: TERM_ID, projectKey: "CHK", name: "Brake pedal", status: "approved", snippet: "The lever the driver presses" }],
          total: 0,
          limit: 20,
          offset: 0,
        },
      },
    });
    await renderAt("/search?q=brake");
    const terms = await screen.findByRole("region", { name: "Terms" });
    expect(within(terms).getByRole("link", { name: "Brake pedal" })).toHaveAttribute("href", `/p/CHK/glossary/${TERM_ID}`);
    expect(within(terms).getByText("The lever the driver presses")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});
