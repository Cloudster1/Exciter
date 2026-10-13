import { type KeyboardEvent, useId, useState } from "react";
import { useArmatureSearch, type ArmatureIssue } from "@/api/armature";
import { Input, cx } from "@/components/ui";
import { ARMATURE_SEARCH_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { StatusLozenge } from "./IssueChip";

/** A box that finds Armature issues by key or summary as the viewer may see them, and hands the chosen one back;
 * the requirement picker's, over Armature. */
export function IssuePicker({ label, onPick }: { label: string; onPick: (issue: ArmatureIssue) => void }) {
  const p = t.armature.picker;
  const id = useId();
  const listId = `${id}-options`;
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const typed = useDebounced(text.trim(), ARMATURE_SEARCH_DEBOUNCE_MS);
  const found = useArmatureSearch(typed, open);
  const options = found.data?.status === "ok" ? found.data.issues : [];
  const current = Math.min(active, Math.max(options.length - 1, 0));
  const expanded = open && text.trim() !== "";

  function pick(issue: ArmatureIssue) {
    onPick(issue);
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
    <div className="space-y-1" data-issue-picker>
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
          data-issue-picker-input
        />
        <div
          id={listId}
          role="listbox"
          aria-label={p.options}
          hidden={!expanded || options.length === 0}
          className="absolute inset-x-0 top-full z-30 mt-1 max-h-64 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
        >
          {options.map((issue, i) => (
            <div
              key={issue.key}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              tabIndex={-1}
              // The press would take focus from the box and close the list before the click lands.
              onMouseDown={(event) => {
                event.preventDefault();
                pick(issue);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx(
                "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
                i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
              )}
              data-issue-option={issue.key}
            >
              <span className="shrink-0 font-mono text-xs">{issue.key}</span>
              <span className="min-w-0 truncate font-medium text-ink">{issue.summary}</span>
              <span className="ml-auto shrink-0">
                <StatusLozenge name={issue.status.name} category={issue.status.category} />
              </span>
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
