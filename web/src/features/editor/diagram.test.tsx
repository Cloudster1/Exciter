import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DIAGRAM_DEFAULT_SOURCE } from "@/config";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { problems } from "@/test/allowlist";
import { DocView } from "./DocView";
import { Editor } from "./Editor";
import type { Doc } from "./schema";

// Diagrams (#284): kept as PlantUML text, drawn by the server, shown as an image, and edited as text with a preview.

const SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><text>Pump</text></svg>';
const SOURCE = "@startuml\ntitle Pump control\n[Pump] --> [Valve]\n@enduml";

const withDiagram = (source: string): Doc => ({
  type: "doc",
  content: [
    { type: "paragraph", content: [{ type: "text", text: "The parts:" }] },
    { type: "diagram", attrs: { language: "plantuml", source } },
  ],
});

function shown(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <main>
        <h1>A requirement</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const drawn: Answer = { status: 200, body: { svg: SVG } };

describe("a diagram read", () => {
  it("is the server's drawing, described by its title, with the drawing to save", async () => {
    const sent = stubApi({ "POST /diagrams": drawn });
    shown(<DocView doc={withDiagram(SOURCE)} />);
    const picture = await screen.findByRole("img", { name: "Pump control" });
    expect(picture.getAttribute("src")).toBe(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(SVG)}`);
    expect(sent).toEqual([{ method: "POST", path: "/diagrams", body: { language: "plantuml", source: SOURCE } }]);
    expect(screen.getByRole("button", { name: "Save as SVG" })).toBeInTheDocument();
    expect(screen.queryByText(/\[Pump\]/)).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("is its source and the renderer's reason when the source cannot be drawn", async () => {
    stubApi({
      "POST /diagrams": {
        status: 422,
        body: { error: { code: "diagram_source", message: "The diagram could not be drawn.", fields: { source: "Syntax Error? (line: 2)" } } },
      },
    });
    shown(<DocView doc={withDiagram(SOURCE)} />);
    expect(await screen.findByText("The diagram cannot be drawn: Syntax Error? (line: 2). Check its source.")).toBeInTheDocument();
    expect(screen.getByText(/\[Pump\] --> \[Valve\]/)).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.queryByRole("button", { name: "Save as SVG" })).toBeNull();
  });

  it("is its source on a server that draws none, saying so", async () => {
    stubApi({ "POST /diagrams": { status: 503, body: { error: { code: "diagrams_unavailable", message: "Diagrams are not drawn." } } } });
    shown(<DocView doc={withDiagram(SOURCE)} />);
    expect(await screen.findByText(/This server draws no diagrams, so the source is shown/)).toBeInTheDocument();
    expect(screen.getByText(/\[Pump\] --> \[Valve\]/)).toBeInTheDocument();
  });
});

describe("a diagram written", () => {
  it("starts from a sketch, is edited as PlantUML and drawn below as it is typed", async () => {
    const sent = stubApi({ "POST /diagrams": drawn });
    let saved: Doc | null = null;
    const user = userEvent.setup();
    shown(<Editor id="statement" value={null} onChange={(doc) => (saved = doc)} aria-label="Statement" />);
    await user.click(await screen.findByRole("button", { name: "PlantUML diagram" }));
    const source = await screen.findByRole("textbox", { name: "PlantUML source" });
    expect(source).toHaveValue(DIAGRAM_DEFAULT_SOURCE);
    expect(await screen.findByRole("img", { name: "Components" }, { timeout: 3000 })).toBeInTheDocument();

    await user.clear(source);
    await user.type(source, SOURCE.replaceAll("[", "[["));
    await waitFor(() => expect(sent.filter((r) => r.path === "/diagrams").at(-1)?.body).toEqual({ language: "plantuml", source: SOURCE }), { timeout: 3000 });
    // One drawing for the sketch and one once typing stopped, not one a key.
    expect(sent.filter((r) => r.path === "/diagrams")).toHaveLength(2);
    const diagram = saved!.content!.find((node) => node.type === "diagram");
    expect(diagram?.attrs).toEqual({ language: "plantuml", source: SOURCE });
    expect(problems(saved!)).toEqual([]);
    expect(await axeViolations()).toEqual([]);
  });

  it("is not offered in a comment", async () => {
    stubApi({});
    shown(<Editor id="comment" value={null} onChange={() => {}} variant="comment" aria-label="Comment" />);
    await screen.findByRole("button", { name: "Bold" });
    expect(screen.queryByRole("button", { name: "PlantUML diagram" })).toBeNull();
  });
});
