import type { HTMLAttributes } from "react";
import { cx } from "./cx";

/** A section heading inside a page, as small capitals. */
export function SectionTitle({ children, className, ...rest }: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h2 {...rest} className={cx("text-2xs font-medium tracking-wide text-ink-subtle uppercase", className)}>
      {children}
    </h2>
  );
}
