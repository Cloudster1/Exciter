import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { useQuickSearch } from "@/api/search";
import { cx } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon, type IconName } from "@/components/icons";
import { QUICK_SEARCH_DEBOUNCE_MS, SEARCH_PATH } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { areaTitle, placesFor } from "./places";

interface Option {
  key: string;
  label: string;
  hint: string;
  icon: IconName;
  pick: () => void;
}

/** Whether every word typed starts a word of the label, so "aud" finds the audit log and "set mem" the members. */
function matches(label: string, typed: string): boolean {
  const words = label.toLowerCase().split(/\s+/);
  return typed
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((part) => words.some((word) => word.startsWith(part)));
}

/** The Ctrl K palette: requirements by key or title as the person types, the places this build has, and a way on
 * to the full search. */
export function QuickSwitcher({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listId = useId();
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);

  const places: Option[] = placesFor(me).map((place) => ({
    key: `place:${place.path}`,
    label: place.label,
    hint: place.area === "work" ? "" : areaTitle(place.area),
    icon: place.icon,
    pick: () => void navigate({ to: place.path }),
  }));
  const typed = text.trim();
  const words = useDebounced(typed, QUICK_SEARCH_DEBOUNCE_MS);
  const quick = useQuickSearch(typed ? words : "");
  // The server matched these already; an answer to an earlier word, kept on show while the newer one loads, is not
  // offered for it.
  const requirements: Option[] =
    typed && words === typed && !quick.isPlaceholderData
      ? (quick.data ?? []).map((hit) => ({
          key: `requirement:${hit.requirement.id}`,
          label: `${hit.requirement.key} ${hit.requirement.title}`,
          hint: [hit.projectName, hit.outline].filter(Boolean).join(" "),
          icon: "ListBullet",
          pick: () => void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: hit.requirement.projectKey, reqKey: hit.requirement.key } }),
        }))
      : [];
  const searchEverything: Option[] = typed
    ? [
        {
          key: "search",
          label: t.search.searchFor(typed),
          hint: t.nav.search,
          icon: "Search",
          pick: () => void navigate({ to: SEARCH_PATH, search: { q: typed } }),
        },
      ]
    : [];
  const options = [...requirements, ...places.filter((option) => matches(`${option.hint} ${option.label}`, typed)), ...searchEverything];
  const current = Math.min(active, options.length - 1);
  const optionId = (index: number) => `${listId}-${index}`;

  // The active option is followed from the keyboard, so the list scrolls itself rather than taking focus.
  useEffect(() => {
    listRef.current?.children[current]?.scrollIntoView?.({ block: "nearest" });
  }, [current]);

  const pick = (option: Option) => {
    onClose();
    option.pick();
  };

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    const last = options.length - 1;
    const moves: Record<string, number> = { ArrowDown: Math.min(current + 1, last), ArrowUp: Math.max(current - 1, 0), Home: 0, End: last };
    if (event.key in moves) {
      event.preventDefault();
      setActive(moves[event.key]!);
    } else if (event.key === "Enter") {
      event.preventDefault();
      const option = options[current];
      if (option) pick(option);
    }
  }

  return createPortal(
    // biome-ignore lint/a11y: the scrim is for the pointer; the keyboard closes the switcher with Escape
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-ink/30 p-4 pt-[10vh]" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={t.switcher.title}
        className="w-full max-w-xl overflow-hidden rounded-overlay border border-border bg-surface-overlay shadow-2"
        data-quick-switcher
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Icon.Search className="shrink-0 text-ink-subtle" />
          <input
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setActive(0);
            }}
            onKeyDown={onKeyDown}
            role="combobox"
            aria-label={t.switcher.label}
            aria-autocomplete="list"
            aria-expanded={options.length > 0}
            aria-controls={options.length > 0 ? listId : undefined}
            aria-activedescendant={options.length > 0 ? optionId(current) : undefined}
            placeholder={t.switcher.placeholder}
            autoComplete="off"
            spellCheck={false}
            className="h-11 min-w-0 flex-1 bg-transparent text-sm text-ink placeholder:text-ink-subtle focus:outline-none"
            data-quick-switcher-input
          />
          <kbd className="hidden font-mono text-2xs text-ink-subtle shell:inline">{t.switcher.escape}</kbd>
        </div>
        <div className="p-1">
          {options.length > 0 ? (
            <div ref={listRef} id={listId} role="listbox" aria-label={t.switcher.options} className="max-h-[50vh] overflow-y-auto">
              {options.map((option, index) => {
                const Glyph = Icon[option.icon];
                return (
                  <div
                    key={option.key}
                    id={optionId(index)}
                    role="option"
                    aria-selected={index === current}
                    tabIndex={-1}
                    onMouseMove={() => index !== current && setActive(index)}
                    // Picked on press, so focus never leaves the input that names the active option.
                    onMouseDown={(e) => {
                      e.preventDefault();
                      pick(option);
                    }}
                    className={cx("flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm", index === current && "bg-accent-subtle")}
                    data-quick-switcher-option={option.key}
                  >
                    <Glyph className="shrink-0 text-ink-muted" />
                    <span className="min-w-0 flex-1 truncate text-ink">{option.label}</span>
                    <span className="max-w-[45%] shrink-0 truncate text-2xs text-ink-muted">{option.hint}</span>
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="px-3 py-6 text-center text-sm text-ink-subtle" data-quick-switcher-empty>
              {t.switcher.noMatch}
            </p>
          )}
          {quick.isError && typed && (
            <p className="px-3 py-2 text-sm text-danger" data-quick-switcher-error>
              {t.search.quickFailed}
            </p>
          )}
        </div>
        <p role="status" className="sr-only">
          {t.switcher.optionsCount(options.length)}
        </p>
      </div>
    </div>,
    document.body,
  );
}
