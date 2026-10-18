import { useEffect, useImperativeHandle, useRef, useState, type FormEvent, type Ref } from "react";
import { EditorContent, useEditor, useEditorState, type Editor as TiptapEditor } from "@tiptap/react";
import type { UploadFile } from "@/api/attachments";
import { Button, Dialog, Field, IconButton, cx } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";
import { EDITOR_HEADING_LEVELS, NEW_TABLE_COLUMNS, NEW_TABLE_ROWS } from "@/config";
import { t } from "@/i18n";
import { commentExtensions, editorExtensions } from "./extensions";
import type { FindingMark } from "./quality";
import { emptyDoc, safeHref, type Doc } from "./schema";
import { mentionSuggestion, type MentionSource } from "./suggestion";

export interface EditorProps {
  /** The id the label points at and the suite types into: the editable element's. */
  id: string;
  value: Doc | null;
  onChange: (doc: Doc) => void;
  /** Who an at sign names; without it the at sign is a plain character. */
  mentions?: MentionSource;
  /** Where a pasted, dropped or picked file goes; without it the editor takes no files. */
  upload?: UploadFile;
  /** comment holds the editor to what a comment may say (#33): no rules, tables or files. */
  variant?: "requirement" | "comment";
  /** The writing rules' findings to mark; undefined keeps the marks there are, moved with the text. */
  findings?: FindingMark[];
  /** What the findings list beside the editor reaches in: selecting a finding's words. */
  controls?: Ref<EditorControls>;
  "aria-label": string;
}

/** What a form holding the editor may ask of it. */
export interface EditorControls {
  showFinding: (finding: Pick<FindingMark, "block" | "from" | "to">) => void;
}

/** The shared editor of statements, rationales and rich text values: Stator's, cut to what a requirement says. */
export function Editor({ id, value, onChange, mentions, upload, variant = "requirement", findings, controls, "aria-label": label }: EditorProps) {
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const mentionsRef = useRef(mentions);
  mentionsRef.current = mentions;
  const editor = useEditor({
    extensions: (() => {
      const suggestion = mentions ? mentionSuggestion((q) => mentionsRef.current?.(q) ?? Promise.resolve([])) : undefined;
      return variant === "comment" ? commentExtensions(suggestion) : editorExtensions(suggestion, upload);
    })(),
    content: value ?? emptyDoc,
    editorProps: {
      attributes: { id, "aria-label": label, "aria-multiline": "true", role: "textbox", class: "doc-content doc-editor px-3 py-2" },
    },
    onUpdate: ({ editor }) => onChangeRef.current(editor.getJSON() as Doc),
  });
  useEffect(() => {
    if (editor && findings && variant === "requirement") editor.commands.setFindings(findings);
  }, [editor, findings, variant]);
  useImperativeHandle(controls, () => ({ showFinding: (finding) => void editor?.chain().showFinding(finding).focus().run() }), [editor]);
  return (
    <div className="rounded-control border border-border-strong bg-surface focus-within:border-focus" data-editor-frame>
      {editor && <Toolbar editor={editor} files={variant === "requirement" && Boolean(upload)} tools={variant === "comment" ? COMMENT_TOOLS : TOOLS} />}
      <EditorContent editor={editor} />
    </div>
  );
}

interface Tool {
  icon: IconName;
  /** Drawn in place of the icon where a glyph would not tell the tools apart, as a heading's level. */
  text?: string;
  label: string;
  active: (e: TiptapEditor) => boolean;
  run: (e: TiptapEditor) => void;
  action: string;
}

const TOOLS: Tool[] = [
  { icon: "Bold", label: t.editor.bold, action: "bold", active: (e) => e.isActive("bold"), run: (e) => e.chain().focus().toggleBold().run() },
  { icon: "Italic", label: t.editor.italic, action: "italic", active: (e) => e.isActive("italic"), run: (e) => e.chain().focus().toggleItalic().run() },
  { icon: "Strike", label: t.editor.strike, action: "strike", active: (e) => e.isActive("strike"), run: (e) => e.chain().focus().toggleStrike().run() },
  { icon: "Code", label: t.editor.code, action: "code", active: (e) => e.isActive("code"), run: (e) => e.chain().focus().toggleCode().run() },
  ...EDITOR_HEADING_LEVELS.map(
    (level): Tool => ({
      icon: "Heading",
      text: `H${level}`,
      label: t.editor.heading(level),
      action: `heading-${level}`,
      active: (e) => e.isActive("heading", { level }),
      run: (e) => e.chain().focus().toggleHeading({ level }).run(),
    }),
  ),
  {
    icon: "ListBullet",
    label: t.editor.bulletList,
    action: "bullet-list",
    active: (e) => e.isActive("bulletList"),
    run: (e) => e.chain().focus().toggleBulletList().run(),
  },
  {
    icon: "ListOrdered",
    label: t.editor.orderedList,
    action: "ordered-list",
    active: (e) => e.isActive("orderedList"),
    run: (e) => e.chain().focus().toggleOrderedList().run(),
  },
  { icon: "Quote", label: t.editor.quote, action: "quote", active: (e) => e.isActive("blockquote"), run: (e) => e.chain().focus().toggleBlockquote().run() },
  {
    icon: "CodeBlock",
    label: t.editor.codeBlock,
    action: "code-block",
    active: (e) => e.isActive("codeBlock"),
    run: (e) => e.chain().focus().toggleCodeBlock().run(),
  },
  { icon: "Rule", label: t.editor.rule, action: "rule", active: () => false, run: (e) => e.chain().focus().setHorizontalRule().run() },
  {
    icon: "Table",
    label: t.editor.table,
    action: "table",
    active: (e) => e.isActive("table"),
    run: (e) => e.chain().focus().insertTable({ rows: NEW_TABLE_ROWS, cols: NEW_TABLE_COLUMNS, withHeaderRow: true }).run(),
  },
];

/** What a comment's toolbar offers: everything but rules and tables, which a comment may not hold. */
const COMMENT_TOOLS = TOOLS.filter((tool) => tool.action !== "rule" && tool.action !== "table");

function Toolbar({ editor, files, tools }: { editor: TiptapEditor; files: boolean; tools: Tool[] }) {
  const [linking, setLinking] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  // The toolbar redraws as the selection moves, so a pressed button says what the caret is in.
  const active = useEditorState({ editor, selector: ({ editor: e }) => tools.map((tool) => tool.active(e)).concat(e.isActive("link")) });
  return (
    <div role="toolbar" aria-label={t.editor.toolbar} className="flex flex-wrap gap-0.5 border-b border-border p-1" data-editor-toolbar>
      {tools.map((tool, i) => {
        const Glyph = Icon[tool.icon];
        return (
          <IconButton
            key={tool.action}
            icon={tool.text ? <span className="text-xs font-semibold">{tool.text}</span> : <Glyph />}
            label={tool.label}
            size="sm"
            aria-pressed={active[i]}
            className={cx(active[i] && "bg-surface-raised text-ink")}
            onClick={() => tool.run(editor)}
            data-action={`format-${tool.action}`}
          />
        );
      })}
      <IconButton
        icon={<Icon.Link />}
        label={t.editor.link}
        size="sm"
        aria-pressed={active[tools.length]}
        className={cx(active[tools.length] && "bg-surface-raised text-ink")}
        onClick={() => setLinking(true)}
        data-action="format-link"
      />
      {files && (
        <>
          <input
            ref={picker}
            type="file"
            multiple
            className="sr-only"
            tabIndex={-1}
            aria-hidden="true"
            onChange={(event) => {
              const chosen = [...(event.target.files ?? [])];
              event.target.value = "";
              editor.chain().focus().uploadFiles(chosen).run();
            }}
            data-editor-file-input
          />
          <IconButton icon={<Icon.Paperclip />} label={t.attachments.insert} size="sm" onClick={() => picker.current?.click()} data-action="format-attach" />
        </>
      )}
      {linking && <LinkDialog editor={editor} onClose={() => setLinking(false)} />}
    </div>
  );
}

/** Asks for the address the selected words point at, and refuses one a browser would run. */
function LinkDialog({ editor, onClose }: { editor: TiptapEditor; onClose: () => void }) {
  const [href, setHref] = useState(String(editor.getAttributes("link").href ?? ""));
  const [problem, setProblem] = useState("");
  useEffect(() => setProblem(""), [href]);
  function save(event: FormEvent) {
    event.preventDefault();
    if (href.trim() === "") {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
      onClose();
      return;
    }
    const safe = safeHref(href.trim());
    if (!safe) {
      setProblem(t.editor.linkInvalid);
      return;
    }
    editor.chain().focus().extendMarkRange("link").setLink({ href: safe }).run();
    onClose();
  }
  return (
    <Dialog title={t.editor.linkTitle} onClose={onClose} data-link-dialog="">
      <form onSubmit={save} className="space-y-4" noValidate>
        <Field label={t.editor.linkAddress} value={href} onChange={(event) => setHref(event.target.value)} error={problem} autoFocus />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" data-action="save-link">
            {t.editor.linkSave}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
