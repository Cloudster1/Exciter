import { useAttributes, useTypes } from "@/api/attributes";
import type { ImportWord } from "@/api/imports";
import { useStatuses } from "@/api/workflows";
import { Card, SelectInput, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { ATTRIBUTE_PREFIX, PRIORITIES, targetLabel } from "./targets";

/** What the file's words mean in the project, Armature's card over types, statuses, priorities and options. */
export function WordsCard({
  projectKey,
  words,
  values,
  onChange,
}: {
  projectKey: string;
  words: ImportWord[];
  values: Record<string, Record<string, string>>;
  onChange: (values: Record<string, Record<string, string>>) => void;
}) {
  const { data: types = [] } = useTypes(projectKey);
  const { data: statuses = [] } = useStatuses(projectKey);
  const { data: attributes = [] } = useAttributes(projectKey);
  if (words.length === 0) return null;

  function optionsFor(target: string): string[] {
    if (target === "type") return types.filter((each) => !each.retired).map((each) => each.name);
    if (target === "status") return statuses.map((each) => each.name);
    if (target === "priority") return [...PRIORITIES];
    const name = target.slice(ATTRIBUTE_PREFIX.length);
    const attribute = attributes.find((each) => each.name.toLowerCase() === name.toLowerCase());
    return (attribute?.options ?? []).filter((o) => !o.retired).map((o) => o.name);
  }

  function pick(target: string, word: string, means: string) {
    onChange({ ...values, [target]: { ...(values[target] ?? {}), [word]: means } });
  }

  return (
    <Card className="p-4" data-import-words>
      <h3 className="mb-1 text-sm font-medium text-ink">{t.imports.wordsTitle}</h3>
      <p className="mb-3 text-sm text-ink-muted">{t.imports.wordsIntro}</p>
      <Table dense aria-label={t.imports.wordsTitle}>
        <thead>
          <tr>
            <Th>{t.imports.becomes}</Th>
            <Th>{t.imports.inFile}</Th>
            <Th>{t.imports.rows}</Th>
            <Th>{t.imports.means}</Th>
          </tr>
        </thead>
        <tbody>
          {words.map((word) => (
            <tr key={`${word.target}:${word.value}`} data-import-word={word.value} data-import-word-matched={word.means ? "" : undefined}>
              <Td className="text-sm text-ink-muted">{targetLabel(word.target)}</Td>
              <Td className="text-sm text-ink">{word.value}</Td>
              <Td className="text-sm text-ink-muted tabular-nums">{word.count}</Td>
              <Td>
                <SelectInput
                  aria-label={t.imports.whatMeans(word.value)}
                  controlSize="sm"
                  value={values[word.target]?.[word.value] ?? word.means}
                  onChange={(event) => pick(word.target, word.value, event.target.value)}
                  data-import-word-means={word.value}
                >
                  <option value="">{t.imports.nothing}</option>
                  {optionsFor(word.target).map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </SelectInput>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </Card>
  );
}
