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
  External: makeIcon("external", ["M9 2.5h4.5V7", "M13.5 2.5 7.5 8.5", "M11 9.5v4H2.5V5h4"]),
  Warning: makeIcon("warning", ["M8 2.5 14 13H2z", "M8 6.5v3", "M8 11.5h.01"]),
  Collapse: makeIcon("collapse", ["M2.5 3h11v10h-11z", "M6 3v10", "m10.5 6.5-1.5 1.5 1.5 1.5"]),
  Expand: makeIcon("expand", ["M2.5 3h11v10h-11z", "M6 3v10", "m9 6.5 1.5 1.5L9 9.5"]),
  Menu: makeIcon("menu", ["M2.5 4h11", "M2.5 8h11", "M2.5 12h11"]),
  X: makeIcon("x", ["m4 4 8 8", "m12 4-8 8"]),
  Plus: makeIcon("plus", ["M8 3v10", "M3 8h10"]),
  Users: makeIcon("users", ["M8 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "M2.5 14a5.5 5.5 0 0 1 11 0"]),
  Check: makeIcon("check", ["m3 8.5 3 3 7-7"]),
  Download: makeIcon("download", ["M8 3v7.5", "m4.5 7 3.5 3.5L11.5 7", "M2.5 13.5h11"]),
  Lock: makeIcon("lock", ["M3.5 7.5h9v6h-9z", "M5.5 7.5v-2a2.5 2.5 0 0 1 5 0v2"]),
  Shield: makeIcon("shield", ["M8 2 3 4v4c0 3 2.2 5 5 6 2.8-1 5-3 5-6V4z", "m6 8 1.5 1.5L10.5 6"]),
  Settings: makeIcon("settings", ["M2.5 4.5h11", "M2.5 11.5h11", "M5.5 3v3", "M10.5 10v3"]),
} as const;

export type IconName = keyof typeof Icon;
