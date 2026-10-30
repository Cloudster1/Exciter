import type { HTMLAttributes } from "react";
import { cx } from "./cx";

export type PageWidth = "narrow" | "content" | "wide";

// Three widths for the whole product: forms, reading, and canvases. A page
// picks one by what it is, not by how much it happens to hold today. The
// first two are wide enough that a table on a large monitor is not a strip
// down the middle, and still capped so a line of prose stays readable.
const widths: Record<PageWidth, string> = {
  narrow: "mx-auto max-w-[64rem]",
  content: "mx-auto max-w-[100rem]",
  wide: "w-full",
};

/** The page's column; data attributes and the like go on through to it. */
export function Page({ width = "content", className, ...rest }: HTMLAttributes<HTMLDivElement> & { width?: PageWidth }) {
  return <div {...rest} className={cx(widths[width], className)} />;
}
