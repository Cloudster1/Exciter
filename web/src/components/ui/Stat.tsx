import type { ReactNode } from "react";
import { cx } from "./cx";
import { ProgressBar, type ProgressTone } from "./ProgressBar";

/**
 * A small figure with its word over it, and a thin bar under it when the
 * figure is a share of something. The column of them on a card is what a
 * reader glances at before reading the card.
 */
export function Stat({
  label,
  value,
  hint,
  share,
  tone = "accent",
  className,
  ...rest
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  /** A fraction from 0 to 1 drawn as a bar under the figure. */
  share?: number;
  tone?: ProgressTone;
  className?: string;
} & Record<`data-${string}`, string | undefined>) {
  return (
    <div {...rest} className={cx("min-w-24", className)}>
      <p className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">{label}</p>
      <p className="text-sm text-ink tabular-nums">
        {value}
        {hint && <span className="ml-1 text-xs text-ink-subtle">{hint}</span>}
      </p>
      {share !== undefined && (
        <ProgressBar value={share * 100} label={`${label}: ${Math.round(share * 100)}%`} size="xs" className="mt-1" segments={[{ value: share * 100, tone }]} />
      )}
    </div>
  );
}
