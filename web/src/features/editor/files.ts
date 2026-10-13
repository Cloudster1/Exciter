import { Extension, Node, mergeAttributes, type JSONContent } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView, type NodeView } from "@tiptap/pm/view";
import { attachmentUrl, isImage, type Attachment, type UploadFile } from "@/api/attachments";
import { IMAGE_ALT_MAX_LENGTH, IMAGE_MAX_WIDTH_PX } from "@/config";
import { t } from "@/i18n";

// Stator's file nodes, without its versions and galleries: a picture stored on the requirement, and a file in
// a line of text.

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    fileUpload: {
      /** Uploads files and puts each where the caret is once it is stored. */
      uploadFiles: (files: File[]) => ReturnType;
    };
  }
}

/** What the API writes an id as; anything else in a pasted document is not one of ours. */
export const ATTACHMENT_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function attachmentId(el: HTMLElement): string | null {
  const id = el.getAttribute("data-attachment-id");
  return id && ATTACHMENT_ID_PATTERN.test(id) ? id : null;
}

/** A width the API takes: a whole number of pixels within its bounds, or none. */
export function imageWidth(value: unknown): number | null {
  const n = typeof value === "string" ? Number.parseInt(value, 10) : value;
  return typeof n === "number" && Number.isInteger(n) && n >= 1 && n <= IMAGE_MAX_WIDTH_PX ? n : null;
}

/** Alternative text within the API's limit, or none. */
export function imageAlt(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? [...value].slice(0, IMAGE_ALT_MAX_LENGTH).join("") : null;
}

function missingImage(): HTMLElement {
  const box = document.createElement("div");
  box.setAttribute("data-image-missing", "");
  box.setAttribute("role", "img");
  box.setAttribute("aria-label", t.attachments.missingImage);
  box.textContent = t.attachments.missingImage;
  return box;
}

/** A picture stored as a file of the requirement, drawn from the API. */
export const Image = Node.create({
  name: "image",
  group: "block",
  atom: true,
  draggable: true,
  selectable: true,
  addAttributes() {
    return {
      attachmentId: { default: null, parseHTML: attachmentId, renderHTML: () => ({}) },
      alt: { default: null, parseHTML: (el) => imageAlt(el.getAttribute("alt")), renderHTML: () => ({}) },
      width: { default: null, parseHTML: (el) => imageWidth(el.getAttribute("width")), renderHTML: () => ({}) },
    };
  },
  // Only pictures of this site's own requirements; a pasted picture from elsewhere has no file behind it and is
  // dropped rather than hotlinked.
  parseHTML() {
    return [{ tag: "img[data-attachment-id]", getAttrs: (el) => (attachmentId(el) ? null : false) }];
  },
  renderHTML({ node, HTMLAttributes }) {
    const id = String(node.attrs.attachmentId);
    return [
      "img",
      mergeAttributes(HTMLAttributes, {
        src: attachmentUrl(id, true),
        alt: node.attrs.alt ?? "",
        width: node.attrs.width ?? undefined,
        "data-attachment-id": id,
      }),
    ];
  },
  addNodeView() {
    return ({ node }) => imageView(node);
  },
});

function imageView(initial: PMNode): NodeView {
  let node = initial;
  let broken = false;
  const dom = document.createElement("figure");
  dom.setAttribute("data-image", "");
  const draw = () => {
    const id = node.attrs.attachmentId as string;
    dom.setAttribute("data-attachment-id", id);
    if (broken) {
      dom.replaceChildren(missingImage());
      return;
    }
    const img = document.createElement("img");
    img.src = attachmentUrl(id, true);
    img.alt = (node.attrs.alt as string | null) ?? "";
    img.draggable = false;
    const width = imageWidth(node.attrs.width);
    if (width) img.style.width = `${width}px`;
    img.addEventListener("error", () => {
      broken = true;
      draw();
    });
    dom.replaceChildren(img);
  };
  draw();
  return {
    dom,
    update: (next) => {
      if (next.type !== node.type) return false;
      if (next.attrs.attachmentId !== node.attrs.attachmentId) broken = false;
      node = next;
      draw();
      return true;
    },
  };
}

/** A file stored on the requirement, in a line of text as its name, which downloads it. */
export const AttachmentChip = Node.create({
  name: "attachment",
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  addAttributes() {
    return {
      attachmentId: { default: null, parseHTML: attachmentId, renderHTML: () => ({}) },
      fileName: { default: null, parseHTML: (el) => el.textContent?.trim() || null, renderHTML: () => ({}) },
    };
  },
  parseHTML() {
    return [{ tag: "a[data-attachment-id], span[data-attachment-id]", getAttrs: (el) => (attachmentId(el) ? null : false) }];
  },
  renderHTML({ node, HTMLAttributes }) {
    const id = String(node.attrs.attachmentId);
    return ["a", mergeAttributes(HTMLAttributes, { href: attachmentUrl(id), "data-attachment-id": id }), String(node.attrs.fileName ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.fileName ?? "");
  },
});

/** The node that stands for a stored file: a picture for an image the browser shows, a name for anything else. */
export function attachmentNode(attachment: Attachment): JSONContent {
  return isImage(attachment.contentType)
    ? { type: "image", attrs: { attachmentId: attachment.id, alt: null, width: null } }
    : { type: "attachment", attrs: { attachmentId: attachment.id, fileName: attachment.fileName } };
}

type Placeholders = { set: DecorationSet };
type PlaceholderMeta = { add: { key: number; pos: number; dom: HTMLElement } } | { remove: number };

const uploadsKey = new PluginKey<Placeholders>("fileUpload");
let nextPlaceholder = 1;

function placeholderPos(state: EditorState, key: number): number | null {
  const found = uploadsKey.getState(state)?.set.find(undefined, undefined, (spec) => spec.upload === key);
  return found?.length ? found[0]!.from : null;
}

// A picture copied out of a browser comes with markup naming it too; a document copied from an office program
// comes with a picture of itself. The first is the file, the second is text.
function pastedFiles(data: DataTransfer | null): File[] {
  if (!data?.files?.length) return [];
  const html = data.getData("text/html");
  if (html && new DOMParser().parseFromString(html, "text/html").body.textContent?.trim()) return [];
  return [...data.files];
}

export interface FileUploadOptions {
  upload?: UploadFile;
}

/** Drop, paste and the toolbar's picker upload a file first and put its node in once it is stored, where the
 * file was dropped or the caret was, even if the text around it changed meanwhile. */
export const FileUpload = Extension.create<FileUploadOptions>({
  name: "fileUpload",
  addOptions() {
    return { upload: undefined };
  },
  addCommands() {
    return {
      uploadFiles:
        (files) =>
        ({ view, state }) => {
          if (!this.options.upload || files.length === 0) return false;
          start(view, this.options.upload, files, state.selection.from);
          return true;
        },
    };
  },
  addProseMirrorPlugins() {
    const upload = this.options.upload;
    return [
      new Plugin<Placeholders>({
        key: uploadsKey,
        state: {
          init: () => ({ set: DecorationSet.empty }),
          apply: (tr, value) => {
            let set = value.set.map(tr.mapping, tr.doc);
            const meta = tr.getMeta(uploadsKey) as PlaceholderMeta | undefined;
            if (meta && "add" in meta) {
              set = set.add(tr.doc, [Decoration.widget(meta.add.pos, meta.add.dom, { upload: meta.add.key, side: -1 })]);
            } else if (meta && "remove" in meta) {
              set = set.remove(set.find(undefined, undefined, (spec) => spec.upload === meta.remove));
            }
            return { set };
          },
        },
        props: {
          decorations: (state) => uploadsKey.getState(state)?.set,
          handlePaste: (view, event) => {
            const files = pastedFiles(event.clipboardData);
            if (!upload || files.length === 0) return false;
            start(view, upload, files, view.state.selection.from);
            return true;
          },
          handleDrop: (view, event, _slice, moved) => {
            const files = [...((event as DragEvent).dataTransfer?.files ?? [])];
            if (!upload || moved || files.length === 0) return false;
            const at = view.posAtCoords({ left: event.clientX, top: event.clientY });
            event.preventDefault();
            start(view, upload, files, at?.pos ?? view.state.selection.from);
            return true;
          },
        },
      }),
    ];
  },
});

function start(view: EditorView, upload: UploadFile, files: File[], pos: number) {
  for (const file of files) {
    const key = nextPlaceholder++;
    const dom = document.createElement("span");
    dom.setAttribute("data-upload-placeholder", "");
    dom.setAttribute("role", "status");
    dom.textContent = t.attachments.uploading(file.name);
    view.dispatch(view.state.tr.setMeta(uploadsKey, { add: { key, pos, dom } }).setMeta("addToHistory", false));
    void upload(file).then((made) => {
      if (view.isDestroyed) return;
      const at = placeholderPos(view.state, key);
      const tr = view.state.tr.setMeta(uploadsKey, { remove: key });
      if (made && at !== null) {
        const node = view.state.schema.nodeFromJSON(attachmentNode(made));
        const $at = tr.doc.resolve(at);
        // A picture is a block: in the middle of a paragraph it splits it, at either end it goes beside it.
        if (node.isBlock && $at.parent.inlineContent) {
          if ($at.parentOffset === 0) tr.insert($at.before(), node);
          else if ($at.parentOffset === $at.parent.content.size) tr.insert($at.after(), node);
          else tr.split(at).insert(tr.mapping.map(at, -1) + 1, node);
        } else if (!node.isBlock && !$at.parent.inlineContent) {
          tr.insert(at, view.state.schema.nodes.paragraph!.create(null, node));
        } else {
          tr.insert(at, node);
        }
      }
      view.dispatch(tr);
    });
  }
}
