import { type KeyboardEvent, useId, useState } from "react";
import { normalizeLabel, useLabelSuggestions } from "@/api/labels";
import { Input, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LABEL_NAME_MAX_LENGTH, LABEL_SUGGEST_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

interface Option {
  name: string;
  /** Somebody typed it, and no requirement they can see carries it yet. */
  typed: boolean;
  requirements?: number;
}

/**
 * A box that offers the labels the reader can see as they type and hands the
 * chosen one back; Enter or a comma takes what was typed when nothing is picked.
 */
export function LabelCombobox({
  label,
  onPick,
  onRemoveLast,
  exclude = [],
  newOption = t.labels.create,
  hint,
  disabled,
}: {
  label: string;
  onPick: (name: string) => void;
  /** Backspace in an empty box takes the last chosen label off. */
  onRemoveLast?: () => void;
  /** Labels already chosen, which are not offered again. */
  exclude?: string[];
  newOption?: (name: string) => string;
  hint?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const listId = `${id}-options`;
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const typed = useDebounced(text.trim().toLowerCase(), LABEL_SUGGEST_DEBOUNCE_MS);
  const found = useLabelSuggestions(typed, open && !disabled);

  const wanted = normalizeLabel(text);
  // What was typed comes first, so Enter takes exactly that; the answer to an
  // earlier prefix, shown while the next one loads, is narrowed to this one.
  const seen = (found.data ?? []).filter((each) => !exclude.includes(each.name) && each.name.startsWith(wanted.name));
  const exact = seen.find((each) => each.name === wanted.name);
  const options: Option[] = [];
  if (exact) options.push({ name: exact.name, typed: false, requirements: exact.requirements });
  else if (text.trim() && !wanted.problem && !exclude.includes(wanted.name)) options.push({ name: wanted.name, typed: true });
  for (const each of seen) if (each !== exact) options.push({ name: each.name, typed: false, requirements: each.requirements });
  const current = Math.min(active, Math.max(options.length - 1, 0));
  const expanded = open && !disabled;

  function pick(name: string) {
    onPick(name);
    setText("");
    setActive(0);
  }

  function commitTyped() {
    if (!text.trim() || wanted.problem) return;
    if (!exclude.includes(wanted.name)) pick(wanted.name);
    else setText("");
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (!open) setOpen(true);
      else setActive(Math.min(current + 1, options.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive(Math.max(current - 1, 0));
    } else if (event.key === "Enter") {
      // Enter picks rather than submitting a form the box sits in.
      event.preventDefault();
      const option = options[current];
      if (expanded && option) pick(option.name);
      else commitTyped();
    } else if (event.key === ",") {
      event.preventDefault();
      commitTyped();
    } else if (event.key === "Backspace" && text === "" && onRemoveLast) {
      onRemoveLast();
    } else if (event.key === "Escape" && open) {
      // Closes the list, not whatever the box sits in.
      event.stopPropagation();
      setOpen(false);
    }
  }

  const problem = text.trim() && wanted.problem ? (wanted.problem === "tooLong" ? t.labels.tooLong(LABEL_NAME_MAX_LENGTH) : t.labels.invalid) : "";
  const status = !expanded ? "" : problem || (options.length === 0 && text.trim() ? (found.isFetching ? t.labels.loading : t.labels.noMatch) : "");

  return (
    <div className="space-y-1" data-label-combobox>
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
          invalid={Boolean(problem)}
          aria-describedby={hint ? `${id}-hint` : undefined}
          autoComplete="off"
          placeholder={t.labels.placeholder}
          value={text}
          disabled={disabled}
          onChange={(event) => {
            setText(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
          onKeyDown={onKeyDown}
          data-label-input
        />
        <div
          id={listId}
          role="listbox"
          aria-label={t.labels.options}
          hidden={!expanded || options.length === 0}
          className="absolute inset-x-0 top-full z-30 mt-1 max-h-56 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
          data-label-options
        >
          {options.map((option, i) => (
            <div
              key={`${option.typed ? "new" : "seen"}:${option.name}`}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              tabIndex={-1}
              // The press would take focus from the box and close the list before the click lands.
              onMouseDown={(event) => {
                event.preventDefault();
                pick(option.name);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx(
                "flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm",
                i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
              )}
              data-label-option={option.name}
            >
              {option.typed ? <Icon.Plus className="shrink-0 text-ink-subtle" /> : <Icon.Label className="shrink-0 text-ink-subtle" />}
              <span className="font-medium text-ink">{option.typed ? newOption(option.name) : option.name}</span>
              {option.requirements !== undefined && <span className="min-w-0 truncate text-xs text-ink-subtle">{t.labels.used(option.requirements)}</span>}
            </div>
          ))}
        </div>
        <p
          role="status"
          className="absolute inset-x-0 top-full z-30 mt-1 rounded-overlay border border-border bg-surface-overlay px-2 py-1.5 text-sm text-ink-subtle shadow-2 empty:hidden"
          data-label-status
        >
          {options.length === 0 || problem ? status : ""}
        </p>
      </div>
      {hint && (
        <p id={`${id}-hint`} className="text-xs text-ink-subtle">
          {hint}
        </p>
      )}
    </div>
  );
}
