// Adapted from Stator's features/editor/DiagramViews.tsx: Exciter draws PlantUML on the server, so the drawing is
// asked for rather than made here, and shown as an image.
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ApiError } from "@/api/client";
import { useDiagram } from "@/api/diagrams";
import { DIAGRAM_FILE_NAME, DIAGRAM_LANGUAGE, DIAGRAM_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

export const DIAGRAM_NODE = "diagram";

/** A source the API takes: something other than spaces, cut at its limit; null otherwise. */
export function diagramSource(value: unknown): string | null {
  if (typeof value !== "string" || !value.trim()) return null;
  return [...value].slice(0, DIAGRAM_MAX_LENGTH).join("");
}

/** The language a diagram is drawn in; PlantUML is the one there is so far. */
export function diagramLanguage(value: unknown): string {
  return typeof value === "string" && value ? value : DIAGRAM_LANGUAGE;
}

const TITLE = /^\s*title\s+(.+?)\s*$/im;

/** The words of a diagram's title line, which describe its picture, as document.DiagramTitle reads them. */
export function diagramTitle(source: string): string | null {
  return TITLE.exec(source)?.[1] ?? null;
}

// A data address, so the picture is an image: nothing in it runs, and the page's image policy already allows it.
export function svgAddress(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

/** Why a diagram is shown as its source, in the reader's language. */
export function whyUndrawn(error: unknown): string {
  if (error instanceof ApiError && error.code === "diagram_source") return t.editor.diagram.unreadable(error.fields.source ?? error.message);
  if (error instanceof ApiError && error.code === "diagrams_unavailable") return t.editor.diagram.unavailable;
  return t.editor.diagram.failed;
}

/** A diagram drawn by the server, or its source while it is drawn and when it cannot be, saying why. */
export function DiagramDrawing({ language, source, delay = 0 }: { language: string; source: string; delay?: number }) {
  const drawn = useDiagram(language, source, delay);
  if (drawn.data && !drawn.isError) {
    return <img className="doc-diagram-img" src={svgAddress(drawn.data)} alt={diagramTitle(source) ?? t.editor.diagram.drawing} data-diagram-svg="" />;
  }
  const failed = drawn.isError;
  return (
    <div className="doc-diagram-source" data-diagram-state={failed ? "error" : "drawing"} aria-busy={failed ? undefined : true}>
      <p className={failed ? "doc-diagram-error" : "doc-diagram-note"} data-diagram-error={failed ? "" : undefined}>
        {failed ? whyUndrawn(drawn.error) : t.editor.diagram.drawingNow}
      </p>
      <pre>
        <code>{source}</code>
      </pre>
    </div>
  );
}

function download(svg: string) {
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = DIAGRAM_FILE_NAME;
  link.click();
  URL.revokeObjectURL(url);
}

/** A diagram as readers see it, with its drawing to save as an SVG file. */
export function DiagramFigure({ language, source }: { language: string; source: string }) {
  const drawn = useDiagram(language, source);
  return (
    <figure className="doc-diagram" data-diagram="">
      <DiagramDrawing language={language} source={source} />
      {drawn.data && !drawn.isError && (
        <figcaption className="doc-diagram-tools" data-print-hide="">
          <Button type="button" variant="ghost" size="sm" onClick={() => download(drawn.data)} data-action="download-diagram">
            <Icon.Download />
            {t.editor.diagram.download}
          </Button>
        </figcaption>
      )}
    </figure>
  );
}
