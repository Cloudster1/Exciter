import { type KeyboardEvent, useId, useState } from "react";
import { useQuickSearch, type QuickHit } from "@/api/search";
import { Input, cx } from "@/components/ui";
import { QUICK_SEARCH_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

/** A box that finds requirements by the start of their key or title, as the Ctrl K palette does, and hands the
 * chosen one back; sections and those named in exclude are not offered. */
export function RequirementPicker({ label, exclude = [], onPick }: { label: string; exclude?: string[]; onPick: (hit: QuickHit) => void }) {
  const id = useId();
  const listId = `${id}-options`;
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const typed = useDebounced(text.trim(), QUICK_SEARCH_DEBOUNCE_MS);
  const found = useQuickSearch(open ? typed : "");
  const options = (found.data ?? []).filter((hit) => hit.requirement.kind === "requirement" && !exclude.includes(hit.requirement.id));
  const current = Math.min(active, Math.max(options.length - 1, 0));
  const expanded = open && text.trim() !== "";

  function pick(hit: QuickHit) {
    onPick(hit);
    setText("");
    setActive(0);
    setOpen(false);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setActive(Math.min(current + 1, options.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive(Math.max(current - 1, 0));
    } else if (event.key === "Enter") {
      // Enter picks rather than submitting the form the box sits in.
      event.preventDefault();
      const option = options[current];
      if (expanded && option) pick(option);
    } else if (event.key === "Escape" && open) {
      // Closes the list, not whatever the box sits in.
      event.stopPropagation();
      setOpen(false);
    }
  }

  const status = expanded && options.length === 0 ? (found.isFetching || typed !== text.trim() ? t.links.loading : t.links.noMatch) : "";

  return (
    <div className="space-y-1" data-requirement-picker>
      <label htmlFor={id} className="block text-sm font-medium text-ink-muted">
        {label}
      </label>
      {/* The list floats over what follows, so opening and closing it never moves a button under the pointer. */}
      <div className="relative">
        <Input
          id={id}
          role="combobox"
          aria-expanded={expanded && options.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={expanded && options[current] ? `${listId}-${current}` : undefined}
          autoComplete="off"
          placeholder={t.links.placeholder}
          value={text}
          onChange={(event) => {
            setText(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
          onKeyDown={onKeyDown}
          data-picker-input
        />
        <div
          id={listId}
          role="listbox"
          aria-label={t.links.options}
          hidden={!expanded || options.length === 0}
          className="absolute inset-x-0 top-full z-30 mt-1 max-h-64 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
        >
          {options.map((hit, i) => (
            <div
              key={hit.requirement.id}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              tabIndex={-1}
              // The press would take focus from the box and close the list before the click lands.
              onMouseDown={(event) => {
                event.preventDefault();
                pick(hit);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx(
                "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
                i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
              )}
              data-picker-option={hit.requirement.key}
            >
              <span className="shrink-0 font-mono text-xs">{hit.requirement.key}</span>
              <span className="min-w-0 truncate font-medium text-ink">{hit.requirement.title}</span>
              <span className="ml-auto shrink-0 truncate text-xs text-ink-subtle">{hit.projectName}</span>
            </div>
          ))}
        </div>
        <p
          role="status"
          className="absolute inset-x-0 top-full z-30 mt-1 rounded-overlay border border-border bg-surface-overlay px-2 py-1.5 text-sm text-ink-subtle shadow-2 empty:hidden"
        >
          {status}
        </p>
      </div>
    </div>
  );
}
