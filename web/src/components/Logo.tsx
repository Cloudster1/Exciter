import { LOGO_WIDTH_PX, MARK_WIDTH_PX } from "@/config";

// The Exciter mark: an E with a spark beside its middle arm, on a tile of the
// accent. public/favicon.svg draws the same shapes in fixed colours.
const TILE = "M7 1h18a6 6 0 0 1 6 6v18a6 6 0 0 1-6 6H7a6 6 0 0 1-6-6V7a6 6 0 0 1 6-6Z";
const LETTER = "M9 8h14v3.5H12.5v3H20V18h-7.5v2.5H23V24H9Z";
const SPARK = "M24.5 14.5h2.5V18h-2.5Z";

function Mark({ size, className, ...data }: { size: number; className?: string } & Record<`data-${string}`, string>) {
  return (
    <svg {...data} viewBox="0 0 32 32" width={size} height={size} aria-hidden="true" focusable="false" className={className}>
      <path d={TILE} className="fill-accent" />
      <path d={LETTER} className="fill-on-accent" />
      <path d={SPARK} className="fill-on-accent" />
    </svg>
  );
}

/** The mark at the size of a page with room for it. Decorative: the name is written beside it. */
export function Logo({ className }: { className?: string }) {
  return <Mark size={LOGO_WIDTH_PX} className={className} data-logo="" />;
}

/** The mark at the size of a line of text, beside the organization's name. */
export function LogoMark({ className }: { className?: string }) {
  return <Mark size={MARK_WIDTH_PX} className={className} data-logo-mark="" />;
}
