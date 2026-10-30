import { cx } from "./cx";

export type ProgressTone = "done" | "progress" | "accent";

const tones: Record<ProgressTone, string> = { done: "bg-status-done", progress: "bg-status-progress", accent: "bg-accent" };

/**
 * A thin bar of one or more segments over a total. The segments wear the
 * status colours the board teaches, so a bar means what a column means.
 */
export function ProgressBar({
  value,
  max = 100,
  segments,
  label,
  size = "sm",
  className,
}: {
  value: number;
  max?: number;
  /** Segments drawn left to right; without them the value is one accent segment. */
  segments?: Array<{ value: number; tone: ProgressTone }>;
  label: string;
  size?: "xs" | "sm";
  className?: string;
}) {
  const total = max || 1;
  const width = (n: number) => `${Math.min(100, Math.max(0, (n / total) * 100))}%`;
  const parts = segments ?? [{ value, tone: "accent" as ProgressTone }];
  return (
    <div
      role="progressbar"
      aria-valuenow={Math.round(value)}
      aria-valuemin={0}
      aria-valuemax={Math.round(max)}
      aria-label={label}
      className={cx("w-full overflow-hidden rounded-full bg-surface-raised", size === "xs" ? "h-1" : "h-2", className)}
    >
      <div className="flex h-full">
        {parts.map((part, i) => (
          <span key={i} className={cx("h-full", tones[part.tone])} style={{ width: width(part.value) }} />
        ))}
      </div>
    </div>
  );
}
