import { useId, type KeyboardEvent, type ReactNode } from "react";
import { cx } from "./cx";

export interface Tab<T extends string> {
  value: T;
  label: ReactNode;
  attrs?: Record<string, string>;
}

// Tabs switch what a region shows without leaving the page. Arrow keys move
// and activate, so a keyboard user does not press Enter on each.
export function Tabs<T extends string>({
  label,
  value,
  tabs,
  onChange,
  panelId,
  className,
}: {
  label: string;
  value: T;
  tabs: Tab<T>[];
  onChange: (value: T) => void;
  /** The TabPanel the tabs switch, when there is one to point at. */
  panelId?: string;
  className?: string;
}) {
  const id = useId();
  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const step =
      event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : event.key === "Home" ? -index : event.key === "End" ? tabs.length - 1 - index : 0;
    if (!step) return;
    event.preventDefault();
    const next = tabs[(index + step + tabs.length) % tabs.length]!;
    onChange(next.value);
    (event.currentTarget.parentElement?.children[tabs.indexOf(next)] as HTMLElement | undefined)?.focus();
  }
  return (
    <div role="tablist" aria-label={label} className={cx("flex gap-1 border-b border-border", className)}>
      {tabs.map((tab, index) => {
        const selected = tab.value === value;
        return (
          <button
            key={tab.value}
            type="button"
            role="tab"
            id={`${id}-${tab.value}`}
            aria-selected={selected}
            aria-controls={panelId}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(tab.value)}
            onKeyDown={(event) => onKeyDown(event, index)}
            {...tab.attrs}
            className={cx(
              "-mb-px border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors",
              selected ? "border-accent font-medium text-ink" : "border-transparent text-ink-muted hover:text-ink",
            )}
          >
            {tab.label}
          </button>
        );
      })}
    </div>
  );
}

export function TabPanel({ id, label, children, className }: { id?: string; label?: string; children: ReactNode; className?: string }) {
  return (
    <div role="tabpanel" id={id} aria-label={label} className={className}>
      {children}
    </div>
  );
}
