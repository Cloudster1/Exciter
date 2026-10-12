import { Icon, type IconName } from "@/components/icons";
import type { TYPE_ICONS } from "@/config";

/** The glyph each type icon is drawn with; an icon this client does not know falls back to the plain one. */
const GLYPHS: Record<(typeof TYPE_ICONS)[number], IconName> = {
  requirement: "Folder",
  interface: "Link",
  constraint: "Lock",
  test: "Check",
  need: "User",
  function: "Code",
  risk: "Warning",
  data: "Table",
};

/** A requirement type at a glance: its icon in its colour. */
export function TypeIcon({ icon, colour, className }: { icon: string; colour: string; className?: string }) {
  const Glyph = Icon[GLYPHS[icon as (typeof TYPE_ICONS)[number]] ?? "Folder"];
  return (
    <span className={className} style={{ color: colour }} aria-hidden="true" data-type-icon={icon}>
      <Glyph />
    </span>
  );
}
