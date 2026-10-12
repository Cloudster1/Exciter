import type { HTMLAttributes } from "react";
import { cx } from "./cx";

/** A quiet tag: a type, a state, a count. Never a call to action. */
export function Tag({ children, className, ...rest }: HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      {...rest}
      className={cx("inline-flex items-center rounded border border-border bg-surface-raised px-1.5 py-px text-2xs font-medium text-ink-muted", className)}
    >
      {children}
    </span>
  );
}
