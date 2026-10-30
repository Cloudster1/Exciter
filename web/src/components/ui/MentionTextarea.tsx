import { useEffect, useMemo, useRef, useState, type ChangeEvent, type KeyboardEvent, type TextareaHTMLAttributes } from "react";
import { Textarea } from "./controls";
import { createPortal } from "react-dom";
import { cx } from "./cx";
import { useAnchored } from "./overlay";
import { t } from "@/i18n";

/** Somebody who can be named: the whole name is what gets inserted. */
export interface Mentionable {
  id: string;
  name: string;
  email?: string;
}

/** The word being typed after the last at sign before the caret, if any. */
export function mentionQuery(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0) return null;
  if (at > 0 && /[\w@]/.test(before[at - 1]!)) return null;
  const query = before.slice(at + 1);
  if (query.includes("\n")) return null;
  return { start: at, query };
}

/** Asks who the text after an at sign may name, a few at most. */
export type FindPeople = (query: string) => Promise<Mentionable[]>;

// A textarea that offers names after an at sign. Arrow keys walk the list,
// Enter or Tab takes one, Escape closes it; the text stays plain, with the
// full name written in, which is what the API reads back as a mention.
export function MentionTextarea({
  value,
  onValueChange,
  findPeople,
  className,
  ...rest
}: Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange"> & {
  value: string;
  onValueChange: (next: string) => void;
  findPeople: FindPeople;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [caret, setCaret] = useState(0);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState<number | null>(null);

  const query = useMemo(() => mentionQuery(value, caret), [value, caret]);
  const [found, setFound] = useState<Mentionable[]>([]);
  const asked = query?.query;
  // The last answer stays while the next is asked, and an answer to older text is dropped.
  useEffect(() => {
    if (asked === undefined) return;
    let current = true;
    findPeople(asked).then(
      (people) => current && setFound(people),
      () => current && setFound([]),
    );
    return () => {
      current = false;
    };
  }, [asked, findPeople]);
  const matches = query ? found : [];
  // A shorter answer than the last leaves the highlight on its last row.
  const highlighted = Math.min(active, matches.length - 1);
  const open = query !== null && matches.length > 0 && dismissed !== query.start;

  function track(el: HTMLTextAreaElement) {
    setCaret(el.selectionStart ?? el.value.length);
  }

  const wrapRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const place = useAnchored(open, wrapRef, listRef);

  function onChange(event: ChangeEvent<HTMLTextAreaElement>) {
    onValueChange(event.target.value);
    track(event.target);
    setActive(0);
    setDismissed(null);
  }

  function take(person: Mentionable) {
    if (!query) return;
    const before = value.slice(0, query.start);
    const after = value.slice(caret);
    const inserted = `@${person.name} `;
    onValueChange(before + inserted + after);
    const next = before.length + inserted.length;
    requestAnimationFrame(() => {
      ref.current?.setSelectionRange(next, next);
      setCaret(next);
    });
    setDismissed(null);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (!open) return;
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActive((i) => (i + 1) % matches.length);
        break;
      case "ArrowUp":
        event.preventDefault();
        setActive((i) => (i - 1 + matches.length) % matches.length);
        break;
      case "Enter":
      case "Tab":
        event.preventDefault();
        take(matches[highlighted]!);
        break;
      case "Escape":
        event.preventDefault();
        event.stopPropagation();
        setDismissed(query?.start ?? null);
        break;
    }
  }

  return (
    <div ref={wrapRef} className="relative">
      <Textarea
        {...rest}
        ref={ref}
        value={value}
        onChange={onChange}
        onKeyDown={onKeyDown}
        onKeyUp={(e) => track(e.currentTarget)}
        onClick={(e) => track(e.currentTarget)}
        className={className}
        aria-autocomplete="list"
        aria-expanded={open}
      />
      {open &&
        createPortal(
          <div
            ref={listRef}
            role="listbox"
            aria-label={t.kit.mentionList}
            data-mention-list
            style={place}
            className="fixed z-30 min-w-56 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
          >
            {matches.map((person, i) => (
              <div
                key={person.id}
                role="option"
                aria-selected={i === highlighted}
                data-mention-option={person.name}
                onMouseDown={(e) => {
                  e.preventDefault();
                  take(person);
                }}
                onMouseEnter={() => setActive(i)}
                className={cx(
                  "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
                  i === highlighted ? "bg-surface-raised text-ink" : "text-ink-muted",
                )}
              >
                <span className="font-medium text-ink">{person.name}</span>
                {person.email && <span className="truncate text-xs text-ink-subtle">{person.email}</span>}
              </div>
            ))}
          </div>,
          document.body,
        )}
    </div>
  );
}
