import { useState } from "react";
import { baselineMatrixHref, NOW, useBaselineMatrix, useBaselines, type Baseline, type BaselineMatrixLink, type BaselineMatrixParams } from "@/api/baselines";
import type { MatrixColumns } from "@/api/coverage";
import type { Project } from "@/api/projects";
import { useStatorPages } from "@/api/stator";
import { ButtonLink, EmptyState, ErrorBanner, Segmented, Select, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MATRIX_PAGE_SIZE } from "@/config";
import { Pages } from "@/features/coverage/Pages";
import { columnKey, columnName } from "@/features/matrix/address";
import { t } from "@/i18n";

const changeTone: Record<NonNullable<BaselineMatrixLink["change"]>, string> = {
  added: "bg-success-subtle text-success",
  removed: "bg-danger-subtle text-danger line-through",
  changed: "bg-warning-subtle text-warning",
};

/** How one link of a baseline's cell reads: its name, suspect or not recorded, and how it changed when compared. */
function linkText(l: BaselineMatrixLink): string {
  let text = l.name;
  if (l.suspect === true) text += ` (${t.coverage.suspect})`;
  if (l.suspect === null) text += ` (${t.baseline.notRecordedMark})`;
  if (l.change) text += ` (${t.baseline.changeMarks[l.change]})`;
  return text;
}

/** A baseline's traceability matrix (#153), as it was taken or compared with now or another baseline. */
export function BaselineMatrixView({ project, baseline }: { project: Project; baseline: Baseline }) {
  const [columns, setColumns] = useState<MatrixColumns>("requirements");
  const [compare, setCompare] = useState("");
  const [offset, setOffset] = useState(0);
  const { data: all } = useBaselines(project.key);
  const params: BaselineMatrixParams = {};
  if (columns !== "requirements") params.columns = columns;
  if (compare) params.with = compare;
  const { data, error, refetch, isPlaceholderData } = useBaselineMatrix(baseline.id, params, offset);
  const pageIds = (data?.columns ?? []).flatMap((c) => (c.page ? [c.page.id] : []));
  const lookup = useStatorPages(pageIds, pageIds.length > 0);
  const titles = new Map([...lookup.pages].flatMap(([id, page]) => (page ? [[id, page.title] as const] : [])));
  return (
    <div className="space-y-3" data-baseline-matrix="">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-wrap items-end gap-3">
          <Segmented<MatrixColumns>
            label={t.coverage.columns}
            value={columns}
            size="sm"
            onChange={(next) => {
              setColumns(next);
              setOffset(0);
            }}
            options={[
              { value: "requirements", label: t.coverage.columnRequirements },
              { value: "issues", label: t.coverage.columnIssues },
              { value: "pages", label: t.matrix.columnPagesKind },
            ]}
          />
          <div className="w-56">
            <Select
              label={t.baseline.matrixCompare}
              value={compare}
              controlSize="sm"
              onChange={(event) => {
                setCompare(event.target.value);
                setOffset(0);
              }}
            >
              <option value="">{t.baseline.matrixAsTaken}</option>
              <option value={NOW}>{t.baseline.now}</option>
              {(all ?? [])
                .filter((each) => each.id !== baseline.id)
                .map((each) => (
                  <option key={each.id} value={each.id}>
                    {each.name}
                  </option>
                ))}
            </Select>
          </div>
        </div>
        <ButtonLink
          href={baselineMatrixHref(baseline.id, params)}
          download
          variant="secondary"
          size="sm"
          icon={<Icon.Download />}
          data-action="baseline-matrix-csv"
        >
          {t.coverage.download}
        </ButtonLink>
      </div>
      {baseline.detail < 2 && <p className="text-sm text-ink-muted">{t.baseline.notRecorded}</p>}
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : data.columns.length === 0 ? (
        <EmptyState icon={<Icon.Link />} title={t.baseline.noMatrixLinks} />
      ) : (
        <div className="overflow-x-auto" aria-busy={isPlaceholderData || undefined}>
          <table className="border-collapse text-xs" aria-label={t.baseline.matrixTab} data-matrix="">
            <thead>
              <tr>
                <th scope="col" className="sticky left-0 bg-surface py-1 pr-2 text-left font-medium text-ink-muted">
                  {t.coverage.requirements}
                </th>
                {data.columns.map((column) => (
                  <th key={columnKey(column)} scope="col" className="px-1 py-1 font-mono font-medium text-ink-muted [writing-mode:vertical-rl]">
                    {columnName(column, titles)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {data.rows.map((row, r) => (
                <tr key={row.id} className="border-t border-border">
                  <th scope="row" className="sticky left-0 max-w-64 truncate bg-surface py-1 pr-2 text-left font-normal">
                    <span className="font-mono text-ink-muted">{row.key}</span> {row.title}
                  </th>
                  {data.columns.map((column, c) => {
                    const links = data.cells.find((cell) => cell.row === r && cell.column === c)?.links ?? [];
                    const name = columnName(column, titles);
                    const text = links.map(linkText).join(", ");
                    return (
                      <td
                        key={columnKey(column)}
                        className="border-l border-border px-1 text-center"
                        data-cell={`${row.key}:${name}`}
                        title={links.length ? t.baseline.matrixCell(row.key, name, text) : undefined}
                      >
                        {links.map((l, i) => (
                          <span
                            key={i}
                            className={cx("inline-block rounded px-0.5", l.change && changeTone[l.change])}
                            data-change={l.change || undefined}
                            data-suspect={l.suspect === null ? "unknown" : l.suspect ? "" : undefined}
                          >
                            {l.suspect === true ? (
                              <Icon.Warning className="inline size-3.5 text-warning" label={t.baseline.matrixCell(row.key, name, text)} />
                            ) : (
                              <Icon.Check className="inline size-3.5" label={t.baseline.matrixCell(row.key, name, text)} />
                            )}
                          </span>
                        ))}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {data && <Pages offset={offset} shown={data.rows.length} total={data.total} size={MATRIX_PAGE_SIZE} label={t.baseline.matrixTab} onOffset={setOffset} />}
    </div>
  );
}
