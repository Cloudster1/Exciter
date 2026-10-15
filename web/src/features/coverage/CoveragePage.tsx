import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  DIMENSIONS,
  LEVELS,
  matrixHref,
  useCoverage,
  useCoverageRuleActions,
  useCoverageRules,
  useMatrix,
  type CoverageRule,
  type CoverageTotals,
  type Dimension,
  type Level,
  type LevelFilter,
  type Matrix,
  type MatrixColumns,
} from "@/api/coverage";
import { useTypes } from "@/api/attributes";
import type { Project } from "@/api/projects";
import {
  Button,
  ButtonLink,
  Card,
  Checkbox,
  EmptyState,
  ErrorBanner,
  IconButton,
  PageHeader,
  SectionTitle,
  Segmented,
  Select,
  Skeleton,
  Tag,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { COVERAGE_PAGE_SIZE, MATRIX_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

type View = "report" | "matrix";

/** A project's coverage: the report with its rules, or the traceability matrix. */
export function CoveragePage({ project }: { project: Project }) {
  const [view, setView] = useState<View>("report");
  return (
    <div className="mx-auto max-w-5xl space-y-4" data-coverage={project.key}>
      <PageHeader
        title={t.coverage.title}
        meta={t.coverage.intro}
        actions={
          <Segmented
            label={t.coverage.view}
            value={view}
            onChange={setView}
            options={[
              { value: "report", label: t.coverage.report },
              { value: "matrix", label: t.coverage.matrix },
            ]}
          />
        }
      />
      {view === "report" ? (
        <>
          <Report project={project} />
          <Rules project={project} />
        </>
      ) : (
        <MatrixView project={project} />
      )}
    </div>
  );
}

const levelTone: Record<Level, string> = {
  full: "bg-success-subtle text-success",
  partial: "bg-warning-subtle text-warning",
  none: "bg-danger-subtle text-danger",
  na: "",
};

function share(n: number, total: number): number {
  return total === 0 ? 0 : Math.round((n / total) * 100);
}

/** One dimension's totals as a bar of full, partial and none. */
function Bar({ dimension, totals }: { dimension: Dimension; totals: CoverageTotals }) {
  return (
    <div className="space-y-1" data-dimension={dimension}>
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="font-medium text-ink">{t.coverage.dimensions[dimension]}</span>
        <span className="text-ink-muted tabular-nums" data-full-share="">
          {totals.total === 0 ? t.coverage.notAsked : t.coverage.percent(share(totals.full, totals.total))}
        </span>
      </div>
      <div className="flex h-2 overflow-hidden rounded-full bg-surface-sunken" aria-hidden="true">
        <span className="bg-success" style={{ width: `${share(totals.full, totals.total)}%` }} />
        <span className="bg-warning" style={{ width: `${share(totals.partial, totals.total)}%` }} />
        <span className="bg-danger" style={{ width: `${share(totals.none, totals.total)}%` }} />
      </div>
      {totals.total > 0 && <p className="text-xs text-ink-subtle">{t.coverage.totals(totals.full, totals.partial, totals.none)}</p>}
    </div>
  );
}

function Report({ project }: { project: Project }) {
  const [only, setOnly] = useState<LevelFilter>({});
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useCoverage(project.key, only, offset);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  const keep = (dimension: Dimension, level: string) => {
    setOffset(0);
    setOnly({ ...only, [dimension]: level || undefined });
  };
  return (
    <>
      <Card className="grid gap-4 p-4 sm:grid-cols-3">
        {DIMENSIONS.map((d) => (
          <Bar key={d} dimension={d} totals={data.summary[d]} />
        ))}
      </Card>
      <Card className="space-y-3 p-4">
        <div className="grid gap-3 sm:grid-cols-3">
          {DIMENSIONS.map((d) => (
            <Select key={d} label={t.coverage.dimensions[d]} value={only[d] ?? ""} onChange={(event) => keep(d, event.target.value)} controlSize="sm">
              <option value="">{t.coverage.anyLevel}</option>
              {LEVELS.map((level) => (
                <option key={level} value={level}>
                  {t.coverage.levels[level]}
                </option>
              ))}
            </Select>
          ))}
        </div>
        {data.rows.length === 0 ? (
          <EmptyState icon={<Icon.Check />} title={t.coverage.noRows} />
        ) : (
          <ul className="flex flex-col divide-y divide-border" aria-label={t.coverage.requirements}>
            {data.rows.map((row) => (
              <li key={row.requirement.id} className="flex flex-wrap items-center gap-2 py-2" data-coverage-row={row.requirement.key}>
                <Link
                  to="/p/$projectKey/r/$reqKey"
                  params={{ projectKey: project.key, reqKey: row.requirement.key }}
                  className="min-w-0 flex-1 truncate text-sm text-ink hover:text-accent hover:underline"
                >
                  <span className="font-mono text-xs text-ink-muted">{row.requirement.key}</span> {row.requirement.title}
                </Link>
                {DIMENSIONS.map((d) => (
                  <Tag key={d} className={levelTone[row[d]]} data-level={`${d}:${row[d]}`} title={t.coverage.dimensions[d]}>
                    {`${t.coverage.dimensions[d]}: ${t.coverage.levels[row[d]]}`}
                  </Tag>
                ))}
              </li>
            ))}
          </ul>
        )}
        <Pages offset={offset} shown={data.rows.length} total={data.total} size={COVERAGE_PAGE_SIZE} label={t.coverage.requirements} onOffset={setOffset} />
      </Card>
    </>
  );
}

function Pages({
  offset,
  shown,
  total,
  size,
  label,
  onOffset,
}: {
  offset: number;
  shown: number;
  total: number;
  size: number;
  label: string;
  onOffset: (n: number) => void;
}) {
  if (total <= size) return null;
  return (
    <nav aria-label={label} className="flex items-center justify-between gap-2">
      <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => onOffset(Math.max(0, offset - size))}>
        {t.coverage.previous}
      </Button>
      <span className="text-sm text-ink-subtle tabular-nums">{t.coverage.range(offset + 1, offset + shown, total)}</span>
      <Button variant="secondary" size="sm" disabled={offset + size >= total} onClick={() => onOffset(offset + size)}>
        {t.coverage.next}
      </Button>
    </nav>
  );
}

/** What coverage asks of each type; its administrators tick and untick it in place. */
function Rules({ project }: { project: Project }) {
  const { data: rules, error, refetch } = useCoverageRules(project.key);
  const { data: types } = useTypes(project.key);
  const { save, remove } = useCoverageRuleActions(project.key);
  const [adding, setAdding] = useState("");
  const mayKeep = project.can.administer && !project.archivedAt;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!rules) return <Skeleton />;
  const everyRule = rules.find((r) => r.typeId === null);
  const open = (types ?? []).filter((type) => !type.retired && !rules.some((r) => r.typeId === type.id));
  const set = (rule: Pick<CoverageRule, "typeId" | Dimension>) =>
    save.mutate({ typeId: rule.typeId ?? undefined, decomposed: rule.decomposed, implemented: rule.implemented, verified: rule.verified });
  const rows: Array<Pick<CoverageRule, "typeId" | "typeName" | Dimension> & { id?: string }> = everyRule
    ? rules
    : [{ typeId: null, typeName: "", decomposed: false, implemented: false, verified: false }, ...rules];
  return (
    <Card className="space-y-3 p-4" data-coverage-rules="">
      <SectionTitle>{t.coverage.rulesTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.coverage.rulesIntro}</p>
      {rules.length === 0 && <p className="text-sm text-ink-muted">{t.coverage.noRules}</p>}
      {!mayKeep && <p className="text-sm text-ink-muted">{t.coverage.rulesReadOnly}</p>}
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <ul className="flex flex-col divide-y divide-border" aria-label={t.coverage.rulesTitle}>
        {rows.map((rule) => {
          const name = rule.typeId === null ? t.coverage.everyType : rule.typeName;
          return (
            <li key={rule.typeId ?? "every"} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2" data-rule={name}>
              <span className="min-w-40 flex-1 text-sm font-medium text-ink">{name}</span>
              {DIMENSIONS.map((d) => (
                <Checkbox
                  key={d}
                  label={t.coverage.dimensions[d]}
                  checked={rule[d]}
                  disabled={!mayKeep || save.isPending}
                  onChange={(event) => set({ ...rule, [d]: event.target.checked })}
                />
              ))}
              {mayKeep && rule.id && rule.typeId !== null && (
                <IconButton
                  label={t.coverage.removeRule(name)}
                  icon={<Icon.Trash />}
                  size="sm"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(rule.id!)}
                />
              )}
            </li>
          );
        })}
      </ul>
      {mayKeep && open.length > 0 && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (!adding) return;
            save.mutate({ typeId: adding, decomposed: false, implemented: false, verified: false }, { onSuccess: () => setAdding("") });
          }}
        >
          <div className="min-w-48">
            <Select label={t.coverage.addRule} value={adding} onChange={(event) => setAdding(event.target.value)} controlSize="sm">
              <option value="" />
              {open.map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
            </Select>
          </div>
          <Button type="submit" variant="secondary" size="sm" disabled={!adding}>
            {t.coverage.addRuleSend}
          </Button>
        </form>
      )}
    </Card>
  );
}

function columnName(column: Matrix["columns"][number]): string {
  return column.requirement ? column.requirement.key : column.issueKey;
}

/** A page of requirements against what they link to, with the whole matrix as a file. */
function MatrixView({ project }: { project: Project }) {
  const [columns, setColumns] = useState<MatrixColumns>("requirements");
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useMatrix(project.key, columns, offset);
  const pick = (next: MatrixColumns) => {
    setColumns(next);
    setOffset(0);
  };
  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Segmented
          label={t.coverage.columns}
          value={columns}
          onChange={pick}
          size="sm"
          options={[
            { value: "requirements", label: t.coverage.columnRequirements },
            { value: "issues", label: t.coverage.columnIssues },
          ]}
        />
        <ButtonLink href={matrixHref(project.key, columns)} download variant="secondary" size="sm" icon={<Icon.Download />} title={t.coverage.downloadHint}>
          {t.coverage.download}
        </ButtonLink>
      </div>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : data.columns.length === 0 ? (
        <EmptyState icon={<Icon.Link />} title={t.coverage.noLinks} />
      ) : (
        <MatrixTable project={project} matrix={data} />
      )}
      {data && <Pages offset={offset} shown={data.rows.length} total={data.total} size={MATRIX_PAGE_SIZE} label={t.coverage.matrix} onOffset={setOffset} />}
    </Card>
  );
}

function MatrixTable({ project, matrix }: { project: Project; matrix: Matrix }) {
  const cells = new Map(matrix.cells.map((cell) => [`${cell.row}:${cell.column}`, cell.links]));
  return (
    <div className="overflow-x-auto">
      <table className="border-collapse text-xs" aria-label={t.coverage.matrix} data-matrix="">
        <thead>
          <tr>
            <th scope="col" className="sticky left-0 bg-surface py-1 pr-2 text-left font-medium text-ink-muted">
              {t.coverage.requirements}
            </th>
            {matrix.columns.map((column) => (
              <th key={columnName(column)} scope="col" className="px-1 py-1 font-mono font-medium text-ink-muted [writing-mode:vertical-rl]">
                {columnName(column)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {matrix.rows.map((row, r) => (
            <tr key={row.id} className="border-t border-border">
              <th scope="row" className="sticky left-0 max-w-64 truncate bg-surface py-1 pr-2 text-left font-normal">
                <Link
                  to="/p/$projectKey/r/$reqKey"
                  params={{ projectKey: project.key, reqKey: row.key }}
                  className="text-ink hover:text-accent hover:underline"
                >
                  <span className="font-mono text-ink-muted">{row.key}</span> {row.title}
                </Link>
              </th>
              {matrix.columns.map((column, c) => {
                const links = cells.get(`${r}:${c}`);
                const names = (links ?? []).map((l) => (l.suspect ? `${l.name} (${t.coverage.suspect})` : l.name)).join(", ");
                return (
                  <td
                    key={columnName(column)}
                    className="border-l border-border px-1 text-center"
                    data-cell={`${row.key}:${columnName(column)}`}
                    title={links ? t.coverage.cell(row.key, columnName(column), names) : undefined}
                  >
                    {links &&
                      (links.some((l) => l.suspect) ? (
                        <Icon.Warning className="mx-auto size-3.5 text-warning" label={t.coverage.cell(row.key, columnName(column), names)} />
                      ) : (
                        <Icon.Check className="mx-auto size-3.5 text-success" label={t.coverage.cell(row.key, columnName(column), names)} />
                      ))}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
