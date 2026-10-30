import { useDeferredValue, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useQuerySuggestions, type Completion } from "@/api/search";
import { Button, Dialog, IconButton, Input, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { QUERY_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { QUERY_ATTRIBUTE_EXAMPLE, QUERY_EXAMPLES, QUERY_FIELDS, QUERY_FUNCTIONS, QUERY_LINK_EXAMPLE, QUERY_OPERATORS, caretLine } from "./help";

// Adapted from Armature's features/search/QueryInput: the box a query is typed in, completed at the caret from the
// server's catalog, and an error shown under the text with a caret at the character it names.

/** One row under the box: a word to put in, or a requirement to open. */
interface Suggestion {
  kind: "completion" | "requirement";
  text: string;
  detail?: string;
  /** For a completion: the characters it replaces. */
  from?: number;
  to?: number;
  quoted?: boolean;
  /** For a requirement: its key. */
  reqKey?: string;
}

/** Puts a chosen word into the text where the caret's word was, and says where the caret lands. */
export function applyCompletion(text: string, completion: Pick<Completion, "from" | "to" | "quoted">, word: string): { text: string; caret: number } {
  const chars = Array.from(text);
  const closing = completion.quoted && !word.startsWith('"') ? '"' : "";
  const opening = completion.quoted && !word.startsWith('"') ? '"' : "";
  const inserted = `${opening}${word}${closing} `;
  const next = [...chars.slice(0, completion.from), ...Array.from(inserted), ...chars.slice(completion.to)].join("");
  return { text: next, caret: completion.from + Array.from(inserted).length };
}

/** Puts the caret after a picked word, unless a key typed since moved it on. */
function settleCaret(input: HTMLInputElement | null, text: string, caret: number) {
  if (!input) return;
  input.focus();
  if (input.value === text) input.setSelectionRange(caret, caret);
}

/** Reads a query refusal out of whatever the request threw; anything else is not this box's to show. */
export function queryFailure(error: unknown): { message: string; position?: number } | null {
  if (error instanceof ApiError && error.code === "bad_query") return { message: error.message, position: error.position };
  return null;
}

/** Where a grid's query is typed: the list under it offers words at the caret and requirements the text finds. */
export function QueryInput({
  projectKey,
  value,
  onSubmit,
  error,
  name = "grid-query",
  label = t.query.label,
  region,
}: {
  projectKey: string;
  value: string;
  onSubmit: (query: string) => void;
  /** What the grid's request threw; a bad_query is shown under the text. */
  error?: unknown;
  /** Tells two boxes on one page apart, the matrix's rows' and columns' (#148), in their ids and names. */
  name?: string;
  label?: string;
  /** Names the search landmark when a page has two. */
  region?: string;
}) {
  const [draft, setDraft] = useState(value);
  const [help, setHelp] = useState(false);
  const [caret, setCaret] = useState(value.length);
  const [focused, setFocused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  // The list opens on typing or Arrow Down, never on focus alone.
  const [asked, setAsked] = useState(false);
  const [active, setActive] = useState(-1);
  const ref = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  useEffect(() => setDraft(value), [value]);

  const failure = queryFailure(error);
  // The request follows the typing at the pace React can spare, and the last answer stays until the next arrives.
  const deferred = useDeferredValue({ q: draft, at: Math.min(caret, Array.from(draft).length) });
  const showing = focused && asked && !dismissed;
  const { data: suggested } = useQuerySuggestions(projectKey, deferred, showing);

  const rows: Suggestion[] = [];
  if (showing && suggested) {
    const c = suggested.completion;
    for (const w of suggested.words) {
      rows.push({ kind: "completion", text: w.text, detail: w.label || t.query.details[w.detail] || w.detail, from: c.from, to: c.to, quoted: c.quoted });
    }
    for (const hit of suggested.requirements) {
      rows.push({ kind: "requirement", text: hit.requirement.title, detail: hit.requirement.key, reqKey: hit.requirement.key });
    }
  }
  const open = rows.length > 0;

  function track(input: HTMLInputElement) {
    setCaret(input.selectionStart ?? input.value.length);
  }

  function choose(row: Suggestion) {
    if (row.kind === "requirement") {
      setDismissed(true);
      if (row.reqKey) void navigate({ to: "/projects/$projectKey/r/$reqKey", params: { projectKey, reqKey: row.reqKey } });
      return;
    }
    // The row's range was measured on the text the suggestions were asked for, which may be a keystroke behind:
    // replacing up to the caret takes the letters typed since with it.
    const length = Array.from(draft).length;
    const to = Math.max(row.to ?? length, Math.min(caret, length));
    const next = applyCompletion(draft, { from: row.from ?? length, to, quoted: row.quoted ?? false }, row.text);
    setDraft(next.text);
    setCaret(next.caret);
    setActive(-1);
    requestAnimationFrame(() => settleCaret(ref.current, next.text, next.caret));
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (!open) {
      if (event.key === "ArrowDown") {
        setAsked(true);
        setDismissed(false);
      }
      return;
    }
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActive((i) => (i + 1) % rows.length);
        break;
      case "ArrowUp":
        event.preventDefault();
        setActive((i) => (i <= 0 ? rows.length - 1 : i - 1));
        break;
      case "Enter":
        // Nothing picked: Enter runs the query.
        if (active >= 0) {
          event.preventDefault();
          choose(rows[active]!);
        }
        break;
      case "Tab": {
        const first = rows.find((row) => row.kind === "completion");
        if (first && !event.shiftKey) {
          event.preventDefault();
          choose(active >= 0 && rows[active]?.kind === "completion" ? rows[active]! : first);
        }
        break;
      }
      case "Escape":
        event.preventDefault();
        event.stopPropagation();
        setDismissed(true);
        setActive(-1);
        break;
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    setDismissed(true);
    onSubmit(draft.trim());
  }

  const listId = `${name}-${projectKey.toLowerCase()}-suggestions`;
  return (
    // biome-ignore lint/a11y/useSemanticElements: jsdom, which the unit tests run in, does not know the search element yet
    <form role="search" aria-label={region} onSubmit={submit} className="min-w-0 flex-1 space-y-2" noValidate data-query-input>
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Input
            ref={ref}
            type="text"
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value);
              track(event.target);
              setAsked(true);
              setDismissed(false);
              setActive(-1);
            }}
            onKeyDown={onKeyDown}
            onKeyUp={(event) => track(event.currentTarget)}
            onClick={(event) => track(event.currentTarget)}
            onFocus={() => setFocused(true)}
            onBlur={() => {
              setFocused(false);
              setAsked(false);
              setActive(-1);
            }}
            aria-label={label}
            role="combobox"
            aria-expanded={open}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={active >= 0 ? `${listId}-${active}` : undefined}
            aria-describedby={failure ? `${listId}-error` : undefined}
            invalid={Boolean(failure)}
            spellCheck={false}
            autoComplete="off"
            maxLength={QUERY_MAX_LENGTH}
            placeholder={t.query.placeholder}
            className="w-full min-w-0 font-mono text-sm"
            data-query
          />
          {open && (
            <div
              id={listId}
              role="listbox"
              aria-label={t.query.suggestions}
              data-query-suggestions
              className="absolute right-0 left-0 z-30 mt-1 max-h-80 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
            >
              {rows.map((row, i) => (
                <div
                  key={`${row.kind}:${row.text}:${row.detail ?? ""}`}
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === active}
                  data-suggestion={row.kind}
                  data-suggestion-text={row.kind === "requirement" ? row.reqKey : row.text}
                  onMouseDown={(event) => {
                    event.preventDefault();
                    choose(row);
                  }}
                  // Only a pointer that moves picks a row: a list that opens under a resting one would take Enter.
                  onMouseMove={() => active !== i && setActive(i)}
                  className={cx(
                    "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
                    i === active ? "bg-surface-raised text-ink" : "text-ink-muted",
                  )}
                >
                  {row.kind === "requirement" ? (
                    <>
                      <span className="shrink-0 font-mono text-xs text-ink-subtle">{row.detail}</span>
                      <span className="truncate text-ink">{row.text}</span>
                    </>
                  ) : (
                    <>
                      <span className="truncate font-mono text-ink">{row.text}</span>
                      {row.detail && <span className="shrink-0 truncate text-xs text-ink-subtle">{row.detail}</span>}
                    </>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
        <IconButton icon={<Icon.Info />} label={t.query.help} variant="secondary" onClick={() => setHelp(true)} data-action="query-help" />
        <Button type="submit" variant="secondary" icon={<Icon.Search />} data-action="run-query">
          {t.query.run}
        </Button>
      </div>
      {failure && (
        <div className="text-sm" role="alert" id={`${listId}-error`} data-query-error>
          <pre className="m-0 overflow-x-auto font-mono text-sm leading-tight text-ink">
            {value}
            {"\n"}
            <span className="text-danger" data-query-caret>
              {caretLine(failure.position)}
            </span>
          </pre>
          <p className="mt-1 text-danger">{failure.message}</p>
        </div>
      )}
      {help && (
        <Dialog open title={t.query.help} onClose={() => setHelp(false)} size="lg">
          <QueryHelp
            onExample={(example) => {
              setDraft(example);
              setHelp(false);
            }}
          />
        </Dialog>
      )}
    </form>
  );
}

/** What a query can say: the fields, the operators, the functions and a few examples to start from. */
export function QueryHelp({ onExample }: { onExample?: (example: string) => void }) {
  return (
    <div className="space-y-4 text-sm text-ink-muted" data-query-help>
      <table className="w-full text-left">
        <tbody>
          {QUERY_FIELDS.map((field) => (
            <tr key={field.name} className="align-top">
              <td className="pr-3 font-mono text-ink">{field.name}</td>
              <td className="pr-3">{t.query.takes[field.name]}</td>
              <td className="font-mono text-ink-subtle">{field.example}</td>
            </tr>
          ))}
          <tr className="align-top">
            <td className="pr-3 font-mono text-ink">"Name"</td>
            <td className="pr-3">{t.query.attributeTakes}</td>
            <td className="font-mono text-ink-subtle">{QUERY_ATTRIBUTE_EXAMPLE}</td>
          </tr>
          <tr className="align-top">
            <td className="pr-3 text-ink">{t.query.linkName}</td>
            <td className="pr-3">{t.query.linkTakes}</td>
            <td className="font-mono text-ink-subtle">{QUERY_LINK_EXAMPLE}</td>
          </tr>
        </tbody>
      </table>
      <p>
        <span className="text-ink">{t.query.operators}</span> <span className="font-mono">{QUERY_OPERATORS.join("  ")}</span>
      </p>
      <p>
        <span className="text-ink">{t.query.functions}</span> <span className="font-mono">{QUERY_FUNCTIONS.join(", ")}</span>; {t.query.functionsHint}
      </p>
      <p>{t.query.join}</p>
      <p>{t.query.negation}</p>
      <p className="text-ink">{t.query.examples}</p>
      <ul className="space-y-1">
        {QUERY_EXAMPLES.map((example) => (
          <li key={example}>
            <Button variant="link" className="text-left font-mono whitespace-normal" onClick={() => onExample?.(example)}>
              {example}
            </Button>
          </li>
        ))}
      </ul>
    </div>
  );
}
