// Adapted from Stator's features/editor/diagram.tsx, with a language beside the source.
import { useId } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Textarea } from "@/components/ui";
import { DIAGRAM_DEFAULT_SOURCE, DIAGRAM_LANGUAGE, DIAGRAM_MAX_LENGTH, DIAGRAM_PREVIEW_DELAY_MS, DIAGRAM_SOURCE_ROWS } from "@/config";
import { t } from "@/i18n";
import { DIAGRAM_NODE, DiagramDrawing, diagramLanguage, diagramSource } from "./DiagramViews";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    diagram: {
      /** Puts a diagram with a sketch to start from where the caret is. */
      insertDiagram: () => ReturnType;
    };
  }
}

// The source is edited in place and drawn below it as it is typed: the text is what is kept, and the drawing shows
// whether it says what was meant.
function DiagramNodeView({ node, updateAttributes, editor }: NodeViewProps) {
  const id = useId();
  const source = typeof node.attrs.source === "string" ? node.attrs.source : "";
  const kept = diagramSource(source);
  return (
    <NodeViewWrapper className="doc-diagram doc-diagram-edit" data-diagram-edit="" contentEditable={false}>
      <label htmlFor={`${id}-source`} className="doc-diagram-label">
        {t.editor.diagram.source}
      </label>
      <Textarea
        id={`${id}-source`}
        value={source}
        rows={DIAGRAM_SOURCE_ROWS}
        maxLength={DIAGRAM_MAX_LENGTH}
        spellCheck={false}
        autoComplete="off"
        readOnly={!editor.isEditable}
        className="font-mono text-sm"
        data-diagram-source=""
        onChange={(event) => updateAttributes({ source: event.target.value })}
      />
      <section className="doc-diagram-preview" aria-label={t.editor.diagram.preview} aria-live="polite" data-diagram-preview="">
        {kept ? (
          <DiagramDrawing language={diagramLanguage(node.attrs.language)} source={kept} delay={DIAGRAM_PREVIEW_DELAY_MS} />
        ) : (
          <p className="doc-diagram-error">{t.editor.diagram.empty}</p>
        )}
      </section>
    </NodeViewWrapper>
  );
}

// A diagram's pre is read ahead of the code block's rule at the default 50, which would take any pre.
const ABOVE_CODE_BLOCK = 60;

/** A diagram written as PlantUML text (#284), drawn by the server for each reader. */
export const Diagram = Node.create({
  name: DIAGRAM_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    return {
      language: {
        default: DIAGRAM_LANGUAGE,
        parseHTML: (el) => diagramLanguage(el.getAttribute("data-language")),
        renderHTML: (attrs) => ({ "data-language": attrs.language }),
      },
      source: {
        default: DIAGRAM_DEFAULT_SOURCE,
        parseHTML: (el) => diagramSource(el.textContent) ?? DIAGRAM_DEFAULT_SOURCE,
        rendered: false,
      },
    };
  },
  parseHTML() {
    return [
      { tag: "pre[data-diagram]", priority: ABOVE_CODE_BLOCK, preserveWhitespace: "full", getAttrs: (el) => (diagramSource((el as HTMLElement).textContent) ? null : false) },
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["pre", mergeAttributes(HTMLAttributes, { "data-diagram": "" }), String(node.attrs.source ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.source ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(DiagramNodeView);
  },
  addCommands() {
    return {
      insertDiagram:
        () =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: { language: DIAGRAM_LANGUAGE, source: DIAGRAM_DEFAULT_SOURCE } }),
    };
  },
});
