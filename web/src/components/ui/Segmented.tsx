import type { KeyboardEvent, ReactNode } from "react";
import { cx } from "./cx";

// A choice between a few views of the same thing. Each option is a button with
// aria-pressed, which is what the tests and screen readers both read.
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
  size = "md",
}: {
  label: string;
  value: T;
  options: Array<{ value: T; label: ReactNode; attrs?: Record<string, string> }>;
  onChange: (value: T) => void;
  size?: "sm" | "md";
}) {
  // Arrow keys move the choice, so the group works like the radio it stands in for.
  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = options[(index + step + options.length) % options.length];
    if (next) {
      onChange(next.value);
      (event.currentTarget.parentElement?.children[options.indexOf(next)] as HTMLElement | undefined)?.focus();
    }
  }
  return (
    <div className="inline-flex rounded-control border border-border bg-surface p-0.5" role="group" aria-label={label}>
      {options.map((option, index) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={selected}
            onClick={() => onChange(option.value)}
            onKeyDown={(event) => onKeyDown(event, index)}
            {...option.attrs}
            className={cx(
              "rounded-[5px] whitespace-nowrap transition-colors",
              size === "sm" ? "px-2 py-0.5 text-xs" : "px-2.5 py-1 text-sm",
              selected ? "bg-accent-subtle font-medium text-accent" : "text-ink-muted hover:text-ink",
            )}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
