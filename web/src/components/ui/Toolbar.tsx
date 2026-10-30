import type { ReactNode } from "react";
import { cx } from "./cx";

/** One row of controls above a list or a chart, the things that narrow it on the left. */
export function Toolbar({ start, end, label, className }: { start: ReactNode; end?: ReactNode; label: string; className?: string }) {
  return (
    <div role="toolbar" aria-label={label} className={cx("mb-3 flex flex-wrap items-center gap-2", className)}>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{start}</div>
      {end && <div className="flex flex-wrap items-center gap-2 sm:shrink-0">{end}</div>}
    </div>
  );
}
