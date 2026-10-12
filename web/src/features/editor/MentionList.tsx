import { forwardRef, useImperativeHandle, useState } from "react";
import { cx } from "@/components/ui";
import { t } from "@/i18n";
import type { Mentionable } from "./extensions";

export interface MentionListHandle {
  onKeyDown: (event: KeyboardEvent) => boolean;
}

interface MentionListProps {
  items: Mentionable[];
  command: (attrs: { id: string; label: string }) => void;
  clientRect?: (() => DOMRect | null) | null;
}

/** The people an at sign could mean, drawn under the caret; the editor keeps focus and the arrows walk them. */
export const MentionList = forwardRef<MentionListHandle, MentionListProps>(function MentionList({ items, command, clientRect }, ref) {
  const [active, setActive] = useState(0);
  const current = Math.min(active, Math.max(items.length - 1, 0));
  const pick = (person: Mentionable | undefined) => {
    if (person) command({ id: person.id, label: person.name });
  };
  useImperativeHandle(ref, () => ({
    onKeyDown: (event) => {
      if (event.key === "ArrowDown") setActive((current + 1) % Math.max(items.length, 1));
      else if (event.key === "ArrowUp") setActive((current - 1 + items.length) % Math.max(items.length, 1));
      else if (event.key === "Enter" || event.key === "Tab") pick(items[current]);
      else return false;
      return true;
    },
  }));
  const rect = clientRect?.();
  if (items.length === 0 || !rect) return null;
  return (
    <div
      role="listbox"
      aria-label={t.editor.mentions}
      data-mention-list
      className="fixed z-40 min-w-56 rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
      style={{ top: rect.bottom + window.scrollY, left: rect.left + window.scrollX }}
    >
      {items.map((person, i) => (
        <div
          key={person.id}
          role="option"
          aria-selected={i === current}
          tabIndex={-1}
          data-mention-option={person.name}
          // The press would take focus from the editor and close the list before the click lands.
          onMouseDown={(event) => {
            event.preventDefault();
            pick(person);
          }}
          onMouseEnter={() => setActive(i)}
          className={cx(
            "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
            i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
          )}
        >
          <span className="font-medium text-ink">{person.name}</span>
          {person.email && <span className="truncate text-xs text-ink-subtle">{person.email}</span>}
        </div>
      ))}
    </div>
  );
});
