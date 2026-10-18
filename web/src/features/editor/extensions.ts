import type { Extensions } from "@tiptap/core";
import Mention from "@tiptap/extension-mention";
import { Table, TableCell, TableHeader, TableRow } from "@tiptap/extension-table";
import StarterKit from "@tiptap/starter-kit";
import type { SuggestionOptions } from "@tiptap/suggestion";
import type { UploadFile } from "@/api/attachments";
import { EDITOR_HEADING_LEVELS } from "@/config";
import { AttachmentChip, FileUpload, Image } from "./files";
import { QualityMarks } from "./quality";
import { safeHref } from "./schema";
import { TermLink, TermMarks } from "./terms";

/** Who an at sign names, as the mention list offers them. */
export interface Mentionable {
  id: string;
  name: string;
  email?: string;
}

/** The editor's nodes and marks, exactly those of the server's allowlist; a mention suggests people when given
 * how, and files are taken when given where they go. */
export function editorExtensions(suggestion?: Omit<SuggestionOptions<Mentionable>, "editor">, upload?: UploadFile): Extensions {
  return [
    StarterKit.configure({
      heading: { levels: [...EDITOR_HEADING_LEVELS] },
      // Underlining reads as a link, and the server's allowlist has no such mark.
      underline: false,
      link: {
        openOnClick: false,
        autolink: true,
        isAllowedUri: (url) => safeHref(url) !== null,
        HTMLAttributes: { target: "_blank", rel: "noopener noreferrer nofollow", class: null },
      },
    }),
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    Mention.configure({
      HTMLAttributes: { class: "mention" },
      renderText: ({ node }) => `@${String(node.attrs.label ?? "")}`,
      ...(suggestion ? { suggestion } : {}),
    }),
    Image,
    AttachmentChip,
    FileUpload.configure({ upload }),
    QualityMarks,
    TermLink,
    TermMarks,
  ];
}

/** The comment editor's nodes and marks (#33), exactly those of api/comment-allowlist.json: text and its structure,
 * and mentions, never rules, tables or files. */
export function commentExtensions(suggestion?: Omit<SuggestionOptions<Mentionable>, "editor">): Extensions {
  return [
    StarterKit.configure({
      heading: { levels: [...EDITOR_HEADING_LEVELS] },
      underline: false,
      horizontalRule: false,
      link: {
        openOnClick: false,
        autolink: true,
        isAllowedUri: (url) => safeHref(url) !== null,
        HTMLAttributes: { target: "_blank", rel: "noopener noreferrer nofollow", class: null },
      },
    }),
    Mention.configure({
      HTMLAttributes: { class: "mention" },
      renderText: ({ node }) => `@${String(node.attrs.label ?? "")}`,
      ...(suggestion ? { suggestion } : {}),
    }),
  ];
}
