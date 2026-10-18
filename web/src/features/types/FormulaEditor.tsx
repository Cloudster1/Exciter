import { useId, useRef, useState } from "react";
import { useAttributes, useFormulaCheck } from "@/api/attributes";
import { useProjectLinkTypes } from "@/api/links";
import { Button, Input, Labelled, Textarea } from "@/components/ui";
import { FORMULA_CHECK_DEBOUNCE_MS, FORMULA_ROWS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

/** The fields a formula reads of a requirement, as the server's formula language names them. */
const FIELDS = ["title", "status", "category", "type", "owner", "priority", "revision", "created", "updated", "quality", "testResult"];
const FUNCTIONS = ["if(", "coalesce(", "round(", "floor(", "ceil(", "abs(", "min(", "max(", "concat(", "lower(", "upper(", "len(", "today()", "days("];
const AGGREGATES = ["count(children)", "sum(children, ", "avg(children, ", "min(children, ", "max(children, ", "any(children, ", "all(children, "];
/** How many suggestions show at once, the best first. */
const SUGGESTIONS = 8;

/** The word being typed before the caret, a quote opening it included. */
function wordBefore(text: string, caret: number): string {
  return /["']?[A-Za-z_][\w ]*$|["']$/.exec(text.slice(0, caret))?.[0] ?? "";
}

/** A computed attribute's formula as it is written (#213): checked as one types, completed, and tried on a requirement. */
export function FormulaEditor({
  projectKey,
  attributeId,
  value,
  onChange,
  onSections,
  error,
}: {
  projectKey: string;
  attributeId?: string;
  value: string;
  onChange: (formula: string) => void;
  onSections: boolean;
  error?: string;
}) {
  const id = useId();
  const area = useRef<HTMLTextAreaElement>(null);
  const [caret, setCaret] = useState(value.length);
  const [tryOn, setTryOn] = useState("");
  const { data: attributes = [] } = useAttributes(projectKey);
  const { data: linkTypes = [] } = useProjectLinkTypes(projectKey);
  const settled = useDebounced({ formula: value, onSections, attributeId, requirementKey: tryOn.trim() || undefined }, FORMULA_CHECK_DEBOUNCE_MS);
  const { data: check } = useFormulaCheck(projectKey, settled);
  const word = wordBefore(value, caret);
  const bare = word.replace(/^["']/, "").toLowerCase();
  const candidates = [
    ...attributes.filter((a) => a.id !== attributeId && !a.retired).map((a) => `"${a.name}"`),
    ...FIELDS,
    ...FUNCTIONS,
    ...AGGREGATES,
    ...linkTypes.filter((l) => l.enabled && !l.retired).map((l) => `count(links('${l.key}', in))`),
  ];
  const suggestions = word ? candidates.filter((c) => c.replace(/^["']/, "").toLowerCase().startsWith(bare) && c !== word).slice(0, SUGGESTIONS) : [];

  function insert(text: string) {
    const start = caret - word.length;
    const next = value.slice(0, start) + text + value.slice(caret);
    onChange(next);
    const at = start + text.length;
    setCaret(at);
    requestAnimationFrame(() => {
      area.current?.focus();
      area.current?.setSelectionRange(at, at);
    });
  }

  const mistakes = check?.errors ?? [];
  return (
    <div className="space-y-2" data-formula-editor>
      <Labelled id={id} label={t.attributes.formula} hint={t.attributes.formulaHint} error={error}>
        <Textarea
          id={id}
          ref={area}
          rows={FORMULA_ROWS}
          className="font-mono"
          value={value}
          invalid={Boolean(error) || mistakes.length > 0}
          aria-describedby={`${error ? `${id}-error` : `${id}-hint`} ${id}-check`}
          spellCheck={false}
          onChange={(event) => {
            onChange(event.target.value);
            setCaret(event.target.selectionStart);
          }}
          onSelect={(event) => setCaret(event.currentTarget.selectionStart)}
        />
      </Labelled>
      {suggestions.length > 0 && (
        <div role="group" aria-label={t.attributes.suggestions} className="flex flex-wrap gap-1" data-formula-suggestions>
          {suggestions.map((s) => (
            <Button key={s} type="button" size="sm" variant="secondary" className="font-mono" onClick={() => insert(s)} aria-label={t.attributes.insert(s)}>
              {s}
            </Button>
          ))}
        </div>
      )}
      <div id={`${id}-check`} aria-live="polite" className="space-y-1 text-sm" data-formula-check>
        {mistakes.map((m) => (
          <p key={`${m.from}-${m.message}`} className="text-danger" data-formula-mistake={m.from}>
            {t.attributes.mistake(m.from, m.to, m.message)}
          </p>
        ))}
        {check && mistakes.length === 0 && check.kind && (
          <p className="text-ink-muted" data-formula-kind={check.kind}>
            {t.attributes.gives(t.attributes.givesKinds[check.kind] ?? check.kind)}
            {check.reads.length > 0 && `. ${t.attributes.reads(check.reads.join(", "))}`}
          </p>
        )}
        {check?.valueError && <p className="text-ink-muted">{check.valueError}</p>}
        {check && mistakes.length === 0 && settled.requirementKey && !check.valueError && (
          <p className="text-ink" data-formula-value>
            {check.value === null || check.value === undefined ? t.attributes.noValue : t.attributes.tryValue(String(check.value))}
          </p>
        )}
      </div>
      <div className="w-56">
        <Labelled id={`${id}-try`} label={t.attributes.tryOn} hint={t.attributes.tryOnHint}>
          <Input id={`${id}-try`} controlSize="sm" value={tryOn} onChange={(event) => setTryOn(event.target.value)} aria-describedby={`${id}-try-hint`} />
        </Labelled>
      </div>
    </div>
  );
}
