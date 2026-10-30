import type { ReactNode } from "react";
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

export function Page({ width = "content", children, className }: { width?: PageWidth; children: ReactNode; className?: string }) {
  return <div className={cx(widths[width], className)}>{children}</div>;
}
