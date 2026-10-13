import type { ImportColumn } from "@/api/imports";
import { Card, SelectInput, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { MANY_VALUED, targetLabel } from "./targets";

/** Which column of the file fills which target, by position; Armature's, over a requirement's targets. */
export function MappingCard({
  columns,
  targets,
  mapping,
  onChange,
}: {
  columns: ImportColumn[];
  targets: string[];
  mapping: Record<string, number[]>;
  onChange: (mapping: Record<string, number[]>) => void;
}) {
  function holderOf(at: number): string {
    return Object.entries(mapping).find(([, positions]) => positions.includes(at))?.[0] ?? "";
  }

  function pick(at: number, target: string) {
    const next: Record<string, number[]> = {};
    for (const [each, positions] of Object.entries(mapping)) {
      const kept = positions.filter((p) => p !== at);
      if (kept.length) next[each] = kept;
    }
    if (target) next[target] = MANY_VALUED.has(target) ? [...(next[target] ?? []), at].sort((a, b) => a - b) : [at];
    onChange(next);
  }

  return (
    <Card className="p-4" data-import-mapping>
      <h3 className="mb-3 text-sm font-medium text-ink">{t.imports.mappingTitle}</h3>
      <Table dense aria-label={t.imports.mappingTitle}>
        <thead>
          <tr>
            <Th>{t.imports.columnInFile}</Th>
            <Th>{t.imports.becomes}</Th>
            <Th>{t.imports.firstValues}</Th>
          </tr>
        </thead>
        <tbody>
          {columns.map((column) => (
            <tr key={column.at} data-import-column={column.name}>
              <Td className="font-mono text-sm text-ink">{column.name}</Td>
              <Td>
                <SelectInput
                  aria-label={t.imports.whatBecomes(column.name)}
                  controlSize="sm"
                  value={holderOf(column.at)}
                  onChange={(event) => pick(column.at, event.target.value)}
                  data-import-target={column.name}
                >
                  <option value="">{t.imports.skip}</option>
                  {targets.map((target) => (
                    <option key={target} value={target}>
                      {targetLabel(target)}
                    </option>
                  ))}
                </SelectInput>
              </Td>
              <Td className="max-w-xs truncate text-sm text-ink-muted">{column.samples.join(" | ")}</Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </Card>
  );
}
