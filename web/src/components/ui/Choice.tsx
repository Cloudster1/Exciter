import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cx } from "./cx";

type ChoiceProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onSelect"> & {
  /** Present makes it one of a radiogroup; absent, a card that acts when pressed. */
  checked?: boolean;
  onSelect: () => void;
};

/** One option among a few, drawn however its children like; the card below is the usual shape. */
export function Choice({ checked, onSelect, className, children, ...rest }: ChoiceProps) {
  return (
    <button
      {...rest}
      type="button"
      role={checked === undefined ? undefined : "radio"}
      aria-checked={checked}
      onClick={onSelect}
      className={cx("rounded-control text-left transition-colors focus-visible:outline-2 focus-visible:outline-focus", className)}
    >
      {children}
    </button>
  );
}

/** A titled option with a sentence under it, for choosing a kind of thing. */
export function OptionCard({
  title,
  description,
  extra,
  checked,
  className,
  ...rest
}: ChoiceProps & { title: ReactNode; description?: ReactNode; extra?: ReactNode }) {
  return (
    <Choice
      {...rest}
      checked={checked}
      className={cx(
        "rounded-overlay border p-3",
        checked ? "border-accent bg-accent-subtle" : "border-border bg-surface hover:border-border-strong",
        className,
      )}
    >
      <span className={cx("block text-sm font-medium", checked ? "text-accent" : "text-ink")}>{title}</span>
      {description && <span className="mt-1 block text-xs text-ink-muted">{description}</span>}
      {/* Subtle ink is too faint on the accent tint in the dark palette. */}
      {extra && <span className={cx("mt-2 block text-2xs", checked ? "text-ink-muted" : "text-ink-subtle")}>{extra}</span>}
    </Choice>
  );
}
