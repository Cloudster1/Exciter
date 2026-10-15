import { type KeyboardEvent, useId, useState } from "react";
import { useStatorSearch, type StatorPageHit } from "@/api/stator";
import { Input, cx } from "@/components/ui";
import { STATOR_SEARCH_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

/** A box that finds Stator pages by the first words of their titles as the viewer may read them, and hands the chosen
 * one back; the issue picker's, over Stator. */
export function PagePicker({ label, onPick }: { label: string; onPick: (page: StatorPageHit) => void }) {
  const p = t.stator.picker;
  const id = useId();
  const listId = `${id}-options`;
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const typed = useDebounced(text.trim(), STATOR_SEARCH_DEBOUNCE_MS);
  const found = useStatorSearch(typed, open);
  const options = found.data?.status === "ok" ? found.data.pages : [];
  const current = Math.min(active, Math.max(options.length - 1, 0));
  const expanded = open && text.trim() !== "";

  function pick(page: StatorPageHit) {
    onPick(page);
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

  let status = "";
  if (expanded && options.length === 0) {
    if (found.isFetching || typed !== text.trim()) status = p.looking;
    else if (found.data?.status === "unreachable" || found.isError) status = p.unreachable;
    else if (found.data && found.data.status !== "ok") status = p.connect;
    else status = p.noMatch;
  }

  return (
    <div className="space-y-1" data-page-picker>
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
          spellCheck={false}
          placeholder={p.placeholder}
          value={text}
          onChange={(event) => {
            setText(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
          onKeyDown={onKeyDown}
          data-page-picker-input
        />
        <div
          id={listId}
          role="listbox"
          aria-label={p.options}
          hidden={!expanded || options.length === 0}
          className="absolute inset-x-0 top-full z-30 mt-1 max-h-64 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
        >
          {options.map((page, i) => (
            <div
              key={page.id}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              tabIndex={-1}
              // The press would take focus from the box and close the list before the click lands.
              onMouseDown={(event) => {
                event.preventDefault();
                pick(page);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx(
                "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
                i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
              )}
              data-page-option={page.id}
            >
              <span className="shrink-0 font-mono text-xs">{page.spaceKey}</span>
              <span className="min-w-0 truncate font-medium text-ink">{page.title}</span>
              {page.path.length > 0 && <span className="ml-auto min-w-0 truncate text-xs">{page.path.join(" / ")}</span>}
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
