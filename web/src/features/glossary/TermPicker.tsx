import { useId, useState, type KeyboardEvent } from "react";
import type { Editor as TiptapEditor } from "@tiptap/react";
import { useTerms, type Term } from "@/api/glossary";
import { Button, Dialog, Input, Tag, cx } from "@/components/ui";
import { TERM_PICKER_LIMIT, TERM_SEARCH_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

/** Links the selected words to a term of the project's glossary on purpose (#210), for words the matcher would not
 * find; in words already linked it changes the term or takes the link off. */
export function TermPickerDialog({ projectKey, editor, onClose }: { projectKey: string; editor: TiptapEditor; onClose: () => void }) {
  const l = t.glossary.editor;
  const id = useId();
  const listId = `${id}-options`;
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const typed = useDebounced(text.trim(), TERM_SEARCH_DEBOUNCE_MS);
  const { data, isFetching } = useTerms(projectKey, { q: typed, limit: TERM_PICKER_LIMIT });
  const { from, to, empty } = editor.state.selection;
  const linked = editor.isActive("term");
  const words = empty ? "" : editor.state.doc.textBetween(from, to, " ");
  const options = data?.terms ?? [];
  const current = Math.min(active, Math.max(options.length - 1, 0));
  const may = !empty || linked;

  function pick(term: Term) {
    editor.chain().focus().extendMarkRange("term").setTerm(term.id).run();
    onClose();
  }

  // The dialog sits inside the requirement's form in React's tree, whose keys would save or leave it.
  function guard(event: KeyboardEvent<HTMLDivElement>) {
    const save = (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s";
    if (event.key !== "Escape" && !save) return;
    event.stopPropagation();
    event.preventDefault();
    if (event.key === "Escape") onClose();
  }

  function steer(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive(Math.min(current + 1, options.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive(Math.max(current - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      const option = options[current];
      if (option) pick(option);
    }
  }

  return (
    <Dialog title={l.linkTitle} onClose={onClose} data-term-picker="">
      {/* biome-ignore lint/a11y/noStaticElementInteractions: it only keeps the keys of the dialog from the form around it */}
      <div className="space-y-3" onKeyDown={guard}>
        <p className="text-sm text-ink-muted">{may ? (words ? l.linkIntro(words) : "") : l.noSelection}</p>
        {may && (
          <>
            <div className="space-y-1">
              <label htmlFor={id} className="block text-sm font-medium text-ink-muted">
                {l.find}
              </label>
              <Input
                id={id}
                role="combobox"
                aria-expanded={options.length > 0}
                aria-controls={listId}
                aria-autocomplete="list"
                aria-activedescendant={options[current] ? `${listId}-${current}` : undefined}
                autoComplete="off"
                value={text}
                onChange={(event) => {
                  setText(event.target.value);
                  setActive(0);
                }}
                onKeyDown={steer}
                data-term-picker-input
              />
            </div>
            <div id={listId} role="listbox" aria-label={l.options} className="max-h-64 overflow-y-auto rounded-control border border-border p-1">
              {options.map((term, i) => (
                <div
                  key={term.id}
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === current}
                  tabIndex={-1}
                  onMouseDown={(event) => {
                    event.preventDefault();
                    pick(term);
                  }}
                  onMouseEnter={() => setActive(i)}
                  className={cx(
                    "flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm",
                    i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
                  )}
                  data-term-option={term.name}
                >
                  <span className="font-medium text-ink">{term.name}</span>
                  {term.projectKey !== projectKey.toUpperCase() && <Tag>{term.projectKey}</Tag>}
                  {term.status !== "approved" && <Tag>{t.glossary.statuses[term.status]}</Tag>}
                </div>
              ))}
            </div>
            <p role="status" className="text-sm text-ink-subtle empty:hidden">
              {options.length === 0 ? (isFetching ? l.loading : l.noMatch) : ""}
            </p>
          </>
        )}
        <div className="flex justify-end gap-2">
          {linked && (
            <Button
              variant="secondary"
              onClick={() => {
                editor.chain().focus().unsetTerm().run();
                onClose();
              }}
              data-action="unlink-term"
            >
              {l.unlink}
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>
            {t.common.cancel}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
