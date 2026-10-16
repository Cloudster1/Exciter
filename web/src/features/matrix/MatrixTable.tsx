import { Link } from "@tanstack/react-router";
import type { Matrix } from "@/api/coverage";
import type { Project } from "@/api/projects";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** A column's name: the requirement's key, or the issue's. */
export function columnName(column: Matrix["columns"][number]): string {
  return column.requirement ? column.requirement.key : column.issueKey;
}

/** A page of the matrix as a table: rows down, columns across, a mark in every cell with a link. */
export function MatrixTable({ project, matrix }: { project: Project; matrix: Matrix }) {
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
