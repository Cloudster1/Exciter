import type { ReactNode } from "react";
import type { ImportStyleUse } from "@/api/word";
import { WORD_FRONT, WORD_KEYWORDS, WORD_PATTERN, WORD_STYLES, WORD_UNIT, wordTableChoice } from "@/api/word";
import { Card, Checkbox, Field, Radio, Select } from "@/components/ui";
import { t } from "@/i18n";

/** How a Word document is read: what is a requirement, how each table is taken, and what the text before its first heading becomes.
 * Every change hands on the whole map, the schema's choices in it kept. */
export function WordChoicesStep({
  choices,
  defaults,
  styles,
  tables,
  errors,
  onChange,
}: {
  choices: Record<string, string>;
  defaults: Record<string, string>;
  styles: ImportStyleUse[];
  tables: number;
  errors: Record<string, string>;
  onChange: (choices: Record<string, string>) => void;
}) {
  const value = (key: string) => choices[key] ?? defaults[key] ?? "";
  const set = (key: string, to: string) => onChange({ ...defaults, ...choices, [key]: to });
  const chosenStyles = value(WORD_STYLES)
    .split(",")
    .map((each) => each.trim())
    .filter(Boolean);
  const toggleStyle = (id: string, on: boolean) => set(WORD_STYLES, (on ? [...chosenStyles, id] : chosenStyles.filter((each) => each !== id)).join(","));

  return (
    <Card className="space-y-4 p-4" data-word-choices>
      <div className="space-y-1">
        <h3 className="text-sm font-medium text-ink">{t.word.choicesTitle}</h3>
        <p className="text-sm text-ink-muted">{t.word.choicesIntro}</p>
      </div>
      <Radios
        legend={t.word.unit}
        name="word-unit"
        value={value(WORD_UNIT)}
        onPick={(to) => set(WORD_UNIT, to)}
        options={[
          { value: "paragraph", label: t.word.unitParagraph, hint: t.word.unitParagraphHint },
          { value: "heading", label: t.word.unitHeading, hint: t.word.unitHeadingHint },
        ]}
      />
      <fieldset className="space-y-1" data-word-styles>
        <legend className="text-sm font-medium text-ink-muted">{t.word.stylesLegend}</legend>
        {styles.length === 0 ? (
          <p className="text-sm text-ink-subtle">{t.word.noStyles}</p>
        ) : (
          <div className="flex flex-col gap-1">
            {styles.map((style) => (
              <Checkbox
                key={style.id}
                label={t.word.styleUse(style.name, style.count)}
                checked={chosenStyles.includes(style.id)}
                onChange={(event) => toggleStyle(style.id, event.target.checked)}
                data-word-style={style.id}
              />
            ))}
          </div>
        )}
        {errors[WORD_STYLES] && <p className="text-sm text-danger">{errors[WORD_STYLES]}</p>}
      </fieldset>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label={t.word.pattern}
          hint={t.word.patternHint}
          error={errors[WORD_PATTERN]}
          value={value(WORD_PATTERN)}
          onChange={(event) => set(WORD_PATTERN, event.target.value)}
          spellCheck={false}
          className="font-mono"
          data-word-pattern
        />
        <Field
          label={t.word.keywords}
          hint={t.word.keywordsHint(defaults[WORD_KEYWORDS] ?? "")}
          error={errors[WORD_KEYWORDS]}
          value={value(WORD_KEYWORDS)}
          onChange={(event) => set(WORD_KEYWORDS, event.target.value)}
          data-word-keywords
        />
      </div>
      <p className="text-sm text-ink-subtle">{t.word.anyMatch}</p>
      {tables > 0 && (
        <fieldset className="space-y-2" data-word-tables={tables}>
          <legend className="text-sm font-medium text-ink-muted">{t.word.tablesLegend}</legend>
          <div className="grid gap-3 sm:grid-cols-2">
            {Array.from({ length: tables }, (_, i) => i + 1).map((n) => (
              <Select
                key={n}
                label={t.word.table(n)}
                id={`word-table-${n}`}
                error={errors[wordTableChoice(n)]}
                value={value(wordTableChoice(n))}
                onChange={(event) => set(wordTableChoice(n), event.target.value)}
                data-word-table={n}
              >
                <option value="one">{t.word.tableOne}</option>
                <option value="rows">{t.word.tableRows}</option>
              </Select>
            ))}
          </div>
        </fieldset>
      )}
      <Radios
        legend={t.word.front}
        name="word-front"
        value={value(WORD_FRONT)}
        onPick={(to) => set(WORD_FRONT, to)}
        options={[
          { value: "section", label: t.word.frontSection },
          { value: "skip", label: t.word.frontSkip },
        ]}
      />
    </Card>
  );
}

function Radios({
  legend,
  name,
  value,
  options,
  onPick,
}: {
  legend: string;
  name: string;
  value: string;
  options: { value: string; label: string; hint?: ReactNode }[];
  onPick: (value: string) => void;
}) {
  return (
    <fieldset className="space-y-1" data-word-radios={name}>
      <legend className="text-sm font-medium text-ink-muted">{legend}</legend>
      {options.map((option) => (
        <Radio
          key={option.value}
          name={name}
          value={option.value}
          label={option.label}
          hint={option.hint}
          checked={value === option.value}
          onChange={() => onPick(option.value)}
        />
      ))}
    </fieldset>
  );
}
