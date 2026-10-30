import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { useRiskMatrix, useRiskRows, type RiskMatrix, type Side } from "@/api/risks";
import { Button, Card, ErrorBanner, IconButton, SectionTitle, Segmented, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { QueryInput, queryFailure } from "@/features/query/QueryInput";
import { RISK_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { LevelTag } from "./LevelTag";
import { StateTag } from "./StateTag";

type Cell = { severity: number; probability: number };

/** The risk matrix (#260): how many risks fall in each cell of severity and probability, before mitigation or as they
 * stand; a cell opens its risks. */
export function RiskMatrixView({ project, query, onQuery }: { project: Project; query: string; onQuery: (query: string) => void }) {
  const [side, setSide] = useState<Side>("effective");
  const [open, setOpen] = useState<Cell | null>(null);
  const { data, error, refetch } = useRiskMatrix(project.key, { query: query || undefined, side });
  const failed = queryFailure(error);
  return (
    <>
      <QueryInput projectKey={project.key} value={query} error={error} onSubmit={onQuery} />
      <Segmented<Side>
        label={t.risks.sideLabel}
        value={side}
        onChange={(next) => {
          setSide(next);
          setOpen(null);
        }}
        options={[
          { value: "before", label: t.risks.matrixSides.before, attrs: { "data-side": "before" } },
          { value: "effective", label: t.risks.matrixSides.effective, attrs: { "data-side": "effective" } },
        ]}
      />
      {error && !failed && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && <MatrixGrid matrix={data} open={open} onOpen={setOpen} />}
      {data && data.unrated > 0 && (
        <p role="status" className="text-sm text-ink-muted" data-unrated={data.unrated}>
          {t.risks.unrated(data.unrated)}
        </p>
      )}
      {open && <CellRisks project={project} query={query} side={side} cell={open} onClose={() => setOpen(null)} />}
    </>
  );
}

function MatrixGrid({ matrix, open, onOpen }: { matrix: RiskMatrix; open: Cell | null; onOpen: (cell: Cell) => void }) {
  if (matrix.severity.values.length === 0) return <p className="text-sm text-ink-muted">{t.risks.matrixNoScheme}</p>;
  const hasLevels = matrix.cells.some((c) => c.level);
  return (
    <section aria-labelledby="risk-matrix-title" className="space-y-2">
      <h2 id="risk-matrix-title" className="sr-only">
        {t.risks.matrixTitle}
      </h2>
      <p className="text-xs text-ink-muted">{t.risks.matrixViewHint}</p>
      {!hasLevels && <p className="text-xs text-ink-muted">{t.risks.rpnMatrix}</p>}
      <Table aria-labelledby="risk-matrix-title" data-risk-matrix="">
        <thead>
          <tr>
            <Th>
              <span className="sr-only">{matrix.probability.label}</span>
            </Th>
            {matrix.severity.values.map((v) => (
              <Th key={v.value} title={v.name}>
                {matrix.severity.label} {v.value}
              </Th>
            ))}
          </tr>
        </thead>
        <tbody>
          {[...matrix.probability.values].reverse().map((p) => (
            <tr key={p.value}>
              <Th scope="row" title={p.name}>
                {matrix.probability.label} {p.value}
              </Th>
              {matrix.severity.values.map((s) => {
                const cell = matrix.cells.find((c) => c.severity === s.value && c.probability === p.value);
                const count = cell?.count ?? 0;
                const pressed = open?.severity === s.value && open?.probability === p.value;
                return (
                  <Td key={s.value} data-risk-cell={`${s.value}:${p.value}`} data-count={count}>
                    <span className="flex items-center gap-2">
                      {cell?.level && <span aria-hidden="true" className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: cell.level.colour }} />}
                      <Button
                        size="sm"
                        variant={pressed ? "primary" : "ghost"}
                        aria-pressed={pressed}
                        aria-label={t.risks.cellCount(count, s.value, p.value)}
                        disabled={count === 0}
                        onClick={() => onOpen({ severity: s.value, probability: p.value })}
                      >
                        {count}
                      </Button>
                      {cell?.level && <span className="text-xs text-ink-muted">{cell.level.name}</span>}
                    </span>
                  </Td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </Table>
    </section>
  );
}

function CellRisks({ project, query, side, cell, onClose }: { project: Project; query: string; side: Side; cell: Cell; onClose: () => void }) {
  const { data, error } = useRiskRows(project.key, { query: query || undefined, side, ...cell }, 0, RISK_PAGE_SIZE);
  return (
    <Card className="space-y-2 p-4" data-cell-risks={`${cell.severity}:${cell.probability}`}>
      <section aria-labelledby="cell-risks-title" className="space-y-2">
        <div className="flex items-center justify-between gap-2">
          <SectionTitle id="cell-risks-title">{t.risks.cellRisks(cell.severity, cell.probability)}</SectionTitle>
          <IconButton icon={<Icon.X />} label={t.risks.close} size="sm" onClick={onClose} />
        </div>
        {error && <ErrorBanner>{error.message}</ErrorBanner>}
        {!data && !error && <Skeleton />}
        {data && (
          <ul className="space-y-1 text-sm">
            {data.risks.map((r) => (
              <li key={r.risk.id} className="flex flex-wrap items-center gap-2">
                <Link to="/projects/$projectKey/r/$reqKey" params={{ projectKey: project.key, reqKey: r.risk.key }} className="font-medium text-accent">
                  {r.risk.key}
                </Link>
                <span className="text-ink">{r.risk.title}</span>
                <LevelTag level={r.rating.level} />
                <StateTag state={r.rating.state} />
              </li>
            ))}
          </ul>
        )}
      </section>
    </Card>
  );
}
