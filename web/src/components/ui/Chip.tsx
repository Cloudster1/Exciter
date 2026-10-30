import type { ButtonHTMLAttributes } from "react";
import { cx } from "./cx";

/** A filter that is on or off. It says so with aria-pressed, which the tests read. */
export function Chip({ pressed, className, children, title, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { pressed: boolean }) {
  return (
    <button
      {...rest}
      type="button"
      aria-pressed={pressed}
      // A long name is cut short to fit; the whole of it is a hover away.
      title={title ?? (typeof children === "string" ? children : undefined)}
      className={cx(
        "inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-sm whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-focus",
        pressed ? "border-accent bg-accent-subtle font-medium text-accent" : "border-border text-ink-muted hover:border-border-strong hover:text-ink",
        className,
      )}
    >
      <span className="min-w-0 truncate">{children}</span>
    </button>
  );
}
