import type { SVGProps } from "react";
import { ICON_SIZE_PX, ICON_STROKE } from "@/config";

// Glyphs drawn here on a 16px grid rather than an icon library, the same set
// Armature draws: one weight, one corner radius.
export type IconProps = Omit<SVGProps<SVGSVGElement>, "children"> & {
  /** Says what the icon means when it stands alone; beside text it is decoration. */
  label?: string;
  size?: number;
};

export const ICON_SIZE = ICON_SIZE_PX;

function makeIcon(name: string, paths: string[]) {
  function Icon({ label, size = ICON_SIZE, className, ...rest }: IconProps) {
    return (
      // biome-ignore lint/a11y/noSvgWithoutTitle: a glyph without a label is aria-hidden decoration; with one it carries its title
      <svg
        {...rest}
        width={size}
        height={size}
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth={ICON_STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden={label ? undefined : true}
        role={label ? "img" : undefined}
        className={className}
        data-icon={name}
      >
        {label && <title>{label}</title>}
        {paths.map((d) => (
          <path key={d} d={d} />
        ))}
      </svg>
    );
  }
  Icon.displayName = `Icon.${name}`;
  // The glyph's name is what a theme keys a replacement on.
  return Object.assign(Icon, { glyph: name });
}

export const Icon = {
  Home: makeIcon("home", ["M2.5 7.5 8 3l5.5 4.5", "M4 6.5V13h8V6.5", "M6.5 13V9.5h3V13"]),
  Search: makeIcon("search", ["M7 12A5 5 0 1 0 7 2a5 5 0 0 0 0 10Z", "m10.5 10.5 3 3"]),
  User: makeIcon("user", ["M8 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "M2.5 14a5.5 5.5 0 0 1 11 0"]),
  ChevronDown: makeIcon("chevron-down", ["m4 6 4 4 4-4"]),
  ChevronUp: makeIcon("chevron-up", ["m4 10 4-4 4 4"]),
  External: makeIcon("external", ["M9 2.5h4.5V7", "M13.5 2.5 7.5 8.5", "M11 9.5v4H2.5V5h4"]),
  Warning: makeIcon("warning", ["M8 2.5 14 13H2z", "M8 6.5v3", "M8 11.5h.01"]),
  Collapse: makeIcon("collapse", ["M2.5 3h11v10h-11z", "M6 3v10", "m10.5 6.5-1.5 1.5 1.5 1.5"]),
  Expand: makeIcon("expand", ["M2.5 3h11v10h-11z", "M6 3v10", "m9 6.5 1.5 1.5L9 9.5"]),
  Menu: makeIcon("menu", ["M2.5 4h11", "M2.5 8h11", "M2.5 12h11"]),
  X: makeIcon("x", ["m4 4 8 8", "m12 4-8 8"]),
  Plus: makeIcon("plus", ["M8 3v10", "M3 8h10"]),
  Users: makeIcon("users", ["M8 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "M2.5 14a5.5 5.5 0 0 1 11 0"]),
  Bold: makeIcon("bold", ["M4.5 3h4a2.5 2.5 0 0 1 0 5h-4z", "M4.5 8h4.5a2.5 2.5 0 0 1 0 5H4.5z"]),
  Italic: makeIcon("italic", ["M7 3h5", "M4 13h5", "M9.5 3l-3 10"]),
  Strike: makeIcon("strike", [
    "M2.5 8h11",
    "M11 4.5C10.5 3.5 9.4 3 8 3 6.3 3 5 3.9 5 5.2c0 1 .7 1.6 2 2",
    "M5 11c.5 1.2 1.6 2 3.2 2 1.8 0 3-.9 3-2.3 0-.6-.2-1.1-.6-1.4",
  ]),
  Code: makeIcon("code", ["m5.5 4.5-3 3.5 3 3.5", "m10.5 4.5 3 3.5-3 3.5"]),
  Link: makeIcon("link", ["M6.5 9.5a2.5 2.5 0 0 0 3.5 0l2-2a2.5 2.5 0 0 0-3.5-3.5l-.5.5", "M9.5 6.5a2.5 2.5 0 0 0-3.5 0l-2 2a2.5 2.5 0 0 0 3.5 3.5l.5-.5"]),
  Heading: makeIcon("heading", ["M3.5 3v10", "M10.5 3v10", "M3.5 8h7", "M12.5 13h2"]),
  ListBullet: makeIcon("list-bullet", ["M6 4h7.5", "M6 8h7.5", "M6 12h7.5", "M2.5 4h.01", "M2.5 8h.01", "M2.5 12h.01"]),
  ListOrdered: makeIcon("list-ordered", ["M6 4h7.5", "M6 8h7.5", "M6 12h7.5", "M2 3l1-.5V6", "M2 9.5c.3-.5 1.8-.6 1.8.3 0 .7-1.8 1.4-1.8 2.2h2"]),
  Quote: makeIcon("quote", ["M3 4v8", "M6 5h7.5", "M6 8h7.5", "M6 11h5"]),
  CodeBlock: makeIcon("code-block", ["M2.5 3h11v10h-11z", "m6.5 6.5-1.5 1.5 1.5 1.5", "m9.5 6.5 1.5 1.5-1.5 1.5"]),
  Rule: makeIcon("rule", ["M2 8h12"]),
  Table: makeIcon("table", ["M2.5 3h11v10h-11z", "M2.5 6.5h11", "M2.5 10h11", "M6.5 3v10"]),
  Diagram: makeIcon("diagram", ["M2 2.5h5v4H2z", "M9 9.5h5v4H9z", "M4.5 6.5v5H9"]),
  Everyone: makeIcon("everyone", [
    "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12Z",
    "M2 8h12",
    "M8 2c1.6 1.7 2.4 3.7 2.4 6S9.6 12.3 8 14c-1.6-1.7-2.4-3.7-2.4-6S6.4 3.7 8 2Z",
  ]),
  Check: makeIcon("check", ["m3 8.5 3 3 7-7"]),
  Key: makeIcon("key", ["M5.5 12.5a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "m7.6 7.4 5.4-5.4", "m11 4 1.5 1.5", "m9.5 5.5 1.5 1.5"]),
  Copy: makeIcon("copy", ["M5.5 5.5h8v8h-8z", "M10.5 5.5v-3h-8v8h3"]),
  Paperclip: makeIcon("paperclip", ["M13 7.5 8.2 12.3a3 3 0 0 1-4.3-4.2l5.4-5.4a2 2 0 0 1 2.8 2.8L6.7 10.9a1 1 0 0 1-1.4-1.4L10 4.8"]),
  File: makeIcon("file", ["M9 1.5H4.5a1 1 0 0 0-1 1v11a1 1 0 0 0 1 1h7a1 1 0 0 0 1-1V5z", "M9 1.5V5h3.5"]),
  Download: makeIcon("download", ["M8 3v7.5", "m4.5 7 3.5 3.5L11.5 7", "M2.5 13.5h11"]),
  Lock: makeIcon("lock", ["M3.5 7.5h9v6h-9z", "M5.5 7.5v-2a2.5 2.5 0 0 1 5 0v2"]),
  Shield: makeIcon("shield", ["M8 2 3 4v4c0 3 2.2 5 5 6 2.8-1 5-3 5-6V4z", "m6 8 1.5 1.5L10.5 6"]),
  Settings: makeIcon("settings", ["M2.5 4.5h11", "M2.5 11.5h11", "M5.5 3v3", "M10.5 10v3"]),
  Sun: makeIcon("sun", [
    "M8 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z",
    "M8 1.5v1.5",
    "M8 13v1.5",
    "M1.5 8H3",
    "M13 8h1.5",
    "m3.4 3.4 1 1",
    "m11.6 11.6 1 1",
    "m3.4 12.6 1-1",
    "m11.6 4.4 1-1",
  ]),
  Moon: makeIcon("moon", ["M13 9.5A5.5 5.5 0 0 1 6.5 3a5.5 5.5 0 1 0 6.5 6.5Z"]),
  Monitor: makeIcon("monitor", ["M2.5 3h11v8h-11z", "M6 13.5h4", "M8 11v2.5"]),
  Palette: makeIcon("palette", [
    "M8 2.5a5.5 5.5 0 1 0 0 11c.8 0 1.2-.6 1-1.3-.3-.9.3-1.7 1.2-1.7h1.3a2 2 0 0 0 2-2A5.5 5.5 0 0 0 8 2.5Z",
    "M5 7.5h.01",
    "M7 5h.01",
    "M10 5.5h.01",
  ]),
  More: makeIcon("more", ["M3.5 8h.01", "M8 8h.01", "M12.5 8h.01"]),
  Edit: makeIcon("edit", ["m10.5 2.5 3 3-8 8h-3v-3z", "m9 4 3 3"]),
  Trash: makeIcon("trash", ["M2.5 4.5h11", "M5.5 4.5V3h5v1.5", "M4 4.5 4.7 13h6.6l.7-8.5", "M7 7.5v3.5", "M9 7.5v3.5"]),
  Upload: makeIcon("upload", ["M8 10.5V3", "m4.5 6.5 3.5-3.5 3.5 3.5", "M2.5 13.5h11"]),
  Folder: makeIcon("folder", ["M2 4.5A1.5 1.5 0 0 1 3.5 3h2.6l1.5 1.5h4.9A1.5 1.5 0 0 1 14 6v5.5a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 11.5z"]),
  Archive: makeIcon("archive", ["M2 3.5h12v3H2z", "M3 6.5V13h10V6.5", "M6.5 9h3"]),
  Label: makeIcon("label", ["M2.5 2.5h5.3l5.7 5.7-5.3 5.3-5.7-5.7z", "M5.5 5.5h.01"]),
  Share: makeIcon("share", ["M8 9.5V2.5", "m5 5.5 3-3 3 3", "M3.5 8v5h9V8"]),
  Comment: makeIcon("comment", ["M2.5 3.5h11v7.5h-6l-3 2.5V11h-2z"]),
  Bell: makeIcon("bell", ["M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3z", "M6.5 13.5a1.5 1.5 0 0 0 3 0"]),
  Eye: makeIcon("eye", ["M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8Z", "M8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z"]),
  Info: makeIcon("info", ["M8 14.5a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13Z", "M8 7.5v4", "M8 5h.01"]),
  Book: makeIcon("book", ["M8 4.5C6.5 3.3 4.5 3 2.5 3v9.5c2 0 4 .3 5.5 1.5 1.5-1.2 3.5-1.5 5.5-1.5V3c-2 0-4 .3-5.5 1.5Z", "M8 4.5V14"]),
} as const;

export type IconName = keyof typeof Icon;
