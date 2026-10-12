import type { HTMLAttributes, ReactNode } from "react";
import { cx } from "./cx";

// The rest of the props are forwarded rather than dropped, so a caller can put
// an id or a data attribute on a card without another element to carry them.
export function Card({
  children,
  className,
  elevated = false,
  actions,
  ...rest
}: HTMLAttributes<HTMLDivElement> & {
  children: ReactNode;
  /** Glass over the backdrop, lifted by the third shadow: a card in a list of cards. */
  elevated?: boolean;
  /** Icon buttons in the top right corner, over the content. */
  actions?: ReactNode;
}) {
  return (
    <div
      {...rest}
      className={cx(
        "rounded-overlay border",
        elevated ? "border-border/60 bg-surface-glass shadow-3 backdrop-blur-md" : "border-border bg-surface",
        actions ? "relative" : null,
        className,
      )}
    >
      {actions && (
        <div className="absolute top-3 right-3 flex items-center gap-0.5" data-card-actions>
          {actions}
        </div>
      )}
      {children}
    </div>
  );
}
