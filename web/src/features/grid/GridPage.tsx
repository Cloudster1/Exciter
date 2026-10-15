import { useState } from "react";
import { useAttributes, useTypes } from "@/api/attributes";
import { exportHref } from "@/api/imports";
import type { Project } from "@/api/projects";
import { useGrid } from "@/api/search";
import { useStatuses } from "@/api/workflows";
import { Button, ButtonLink, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { GRID_PAGE_SIZE } from "@/config";
import { SelectionBar, useMayCreateIssues, useSelection } from "@/features/armature/CreateIssues";
import { queryFailure } from "@/features/query/QueryInput";
import { t } from "@/i18n";
import { ColumnChooser } from "./ColumnChooser";
import { DEFAULT_COLUMNS, fromAddress, gridParams, toAddress, type GridAddress } from "./filter";
import { GridFilters } from "./GridFilters";
import { GridTable } from "./GridTable";
import { GridViews } from "./GridViews";

/** A project's requirements as a grid: narrowed, ordered and shown in the columns the address holds, a window at a time. */
export function GridPage({ project, address, onAddress }: { project: Project; address: GridAddress; onAddress: (next: GridAddress) => void }) {
  const { filter, sort } = fromAddress(address);
  const columns = address.columns ?? DEFAULT_COLUMNS;
  const params = gridParams(filter, sort);
  const [offset, setOffset] = useState(0);
  const [seen, setSeen] = useState(JSON.stringify(params));
  // A new narrowing or order starts again at the first window.
  if (seen !== JSON.stringify(params)) {
    setSeen(JSON.stringify(params));
    setOffset(0);
  }
  const { data, error, isLoading, isPlaceholderData, refetch } = useGrid(project.key, params, { limit: GRID_PAGE_SIZE, offset });
  const { data: attributes = [] } = useAttributes(project.key);
  const { data: types = [] } = useTypes(project.key);
  const { data: statuses = [] } = useStatuses(project.key);
  const [notice, setNotice] = useState("");
  const [editError, setEditError] = useState("");
  const keep = { columns: address.columns, view: address.view };
  const selection = useSelection();
  const mayCreate = useMayCreateIssues(project);
  const rows = data?.rows ?? [];

  return (
    <div data-grid={project.key}>
      <PageHeader
        title={t.grid.title}
        meta={t.projects.projectAndKey(project.name, project.key)}
        actions={
          <GridViews
            project={project}
            viewId={address.view}
            state={{ filter, sort, columns }}
            onOpen={(view) =>
              onAddress(view ? toAddress(view.filter, view.sort, { columns: view.columns.length ? view.columns : undefined, view: view.id }) : {})
            }
            onNotice={setNotice}
          />
        }
      />
      <GridFilters
        project={project}
        filter={filter}
        types={types}
        statuses={statuses}
        attributes={attributes}
        queryError={error}
        onFilter={(next) => onAddress(toAddress(next, sort, keep))}
      />
      <div className="mb-2 flex flex-wrap items-start justify-between gap-2">
        <p className="text-sm text-ink-subtle tabular-nums" role="status" data-grid-count>
          {data ? t.grid.count(data.total, data.offset + 1, data.offset + rows.length) : ""}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <ButtonLink
            href={exportHref(project.key, params, columns)}
            download
            variant="secondary"
            size="sm"
            icon={<Icon.Download />}
            title={t.imports.exportHint}
            data-action="export-csv"
          >
            {t.imports.exportCsv}
          </ButtonLink>
          <ColumnChooser columns={columns} attributes={attributes} onChange={(next) => onAddress({ ...toAddress(filter, sort, keep), columns: next })} />
        </div>
      </div>
      <p role="status" className="mb-2 text-sm text-ink-muted empty:hidden" data-grid-notice>
        {notice}
      </p>
      {editError && <ErrorBanner>{editError}</ErrorBanner>}
      {queryFailure(error) ? null : error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton rows={6} />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Table />} title={t.grid.emptyTitle} description={t.grid.emptyBody} />
      ) : (
        <div aria-busy={isPlaceholderData || undefined}>
          <GridTable
            projectKey={project.key}
            rows={rows}
            columns={columns}
            sort={sort}
            attributes={attributes}
            selection={mayCreate ? selection : undefined}
            onSort={(next) => onAddress(toAddress(filter, next, keep))}
            onEdit={(result) => {
              setNotice(result.saved ?? "");
              setEditError(result.error ?? "");
            }}
          />
          {data.total > GRID_PAGE_SIZE && (
            <div className="mt-3 flex items-center justify-end gap-2" data-grid-pages>
              <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - GRID_PAGE_SIZE))}>
                {t.grid.previous}
              </Button>
              <Button variant="secondary" size="sm" disabled={offset + GRID_PAGE_SIZE >= data.total} onClick={() => setOffset(offset + GRID_PAGE_SIZE)}>
                {t.grid.next}
              </Button>
            </div>
          )}
        </div>
      )}
      {mayCreate && <SelectionBar projectKey={project.key} selection={selection} />}
    </div>
  );
}
