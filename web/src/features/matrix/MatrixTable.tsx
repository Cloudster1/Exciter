import { useCallback, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import type { Matrix } from "@/api/coverage";
import type { Project } from "@/api/projects";
import { useStatorPages } from "@/api/stator";
import { Button } from "@/components/ui";
import { useAnchored } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { columnKey, columnName } from "./address";
import { CellPanel } from "./CellPanel";

/** A row's or column's count of links, marked as a gap (#148) when it has none. */
function Count({ n }: { n: number | undefined }) {
  return (
    <td
      className={`px-1 py-1 text-center tabular-nums ${n === 0 ? "bg-danger-subtle font-medium text-danger" : "text-ink-muted"}`}
      title={n === 0 ? t.matrix.noLink : undefined}
      data-gap={n === 0 ? "" : undefined}
    >
      {n ?? ""}
    </td>
  );
}

/** A page of the matrix as a table: rows down, columns across, a mark in every cell with a link, and how many links
 * each row and column has, a gap marked. A cell opens its links in a panel (#149). */
export function MatrixTable({ project, matrix }: { project: Project; matrix: Matrix }) {
  const cells = new Map(matrix.cells.map((cell) => [`${cell.row}:${cell.column}`, cell.links]));
  const [open, setOpen] = useState<{ row: number; column: number } | null>(null);
  const anchor = useRef<HTMLElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);
  const place = useAnchored(open !== null, anchor, panelRef);
  const close = useCallback(() => setOpen(null), []);
  // Pages are named as Stator shows the reader them, each batch looked up once and kept (#150).
  const pageIds = matrix.columns.flatMap((column) => (column.page ? [column.page.id] : []));
  const lookup = useStatorPages(pageIds, pageIds.length > 0);
  const titles = new Map([...lookup.pages].flatMap(([id, page]) => (page ? [[id, page.title] as const] : [])));
  const nameOf = (column: Matrix["columns"][number]) => columnName(column, titles);
  const openRow = open ? matrix.rows[open.row] : undefined;
  const openColumn = open ? matrix.columns[open.column] : undefined;
  return (
    <div className="overflow-x-auto">
      <table className="border-collapse text-xs" aria-label={t.coverage.matrix} data-matrix="">
        <thead>
          <tr>
            <th scope="col" className="sticky left-0 bg-surface py-1 pr-2 text-left font-medium text-ink-muted">
              {t.coverage.requirements}
            </th>
            <th scope="col" className="px-1 py-1 font-medium text-ink-muted [writing-mode:vertical-rl]">
              {t.matrix.links}
            </th>
            {matrix.columns.map((column) => (
              <th key={columnKey(column)} scope="col" className="px-1 py-1 font-mono font-medium text-ink-muted [writing-mode:vertical-rl]">
                {nameOf(column)}
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
              <Count n={matrix.rowLinks[r]} />
              {matrix.columns.map((column, c) => {
                const links = cells.get(`${r}:${c}`);
                const names = (links ?? []).map((l) => (l.suspect ? `${l.name} (${t.coverage.suspect})` : l.name)).join(", ");
                const label = links ? t.coverage.cell(row.key, nameOf(column), names) : t.matrix.openCell(row.key, nameOf(column));
                const opened = open?.row === r && open.column === c;
                return (
                  <td key={columnKey(column)} className="border-l border-border p-0 text-center" data-cell={`${row.key}:${nameOf(column)}`} title={label}>
                    {(links || row.canLink) && (
                      <Button
                        variant="link"
                        className="flex h-6 w-full min-w-6 rounded-none hover:bg-surface-raised focus-visible:outline-2 focus-visible:outline-focus"
                        aria-label={label}
                        aria-haspopup="dialog"
                        aria-expanded={opened}
                        onClick={(event) => {
                          anchor.current = event.currentTarget;
                          setOpen(opened ? null : { row: r, column: c });
                        }}
                        icon={
                          links &&
                          (links.some((l) => l.suspect) ? <Icon.Warning className="size-3.5 text-warning" /> : <Icon.Check className="size-3.5 text-success" />)
                        }
                        data-action="open-cell"
                      />
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="border-t border-border-strong" data-column-links>
            <th scope="row" className="sticky left-0 bg-surface py-1 pr-2 text-left font-medium text-ink-muted">
              {t.matrix.links}
            </th>
            <td />
            {matrix.columns.map((column, c) => (
              <Count key={columnKey(column)} n={matrix.columnLinks[c]} />
            ))}
          </tr>
        </tfoot>
      </table>
      {open && openRow && openColumn && (
        <CellPanel
          key={`${open.row}:${open.column}`}
          projectKey={project.key}
          row={openRow}
          column={openColumn}
          columnName={nameOf(openColumn)}
          linkIds={(cells.get(`${open.row}:${open.column}`) ?? []).map((link) => link.id)}
          anchor={anchor}
          place={place}
          panelRef={panelRef}
          onClose={close}
        />
      )}
    </div>
  );
}
