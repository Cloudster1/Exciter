import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useAttributes, useTypes } from "@/api/attributes";
import { matrixHref, useMatrix } from "@/api/coverage";
import type { Project } from "@/api/projects";
import { useStatuses } from "@/api/workflows";
import { ApiError } from "@/api/client";
import { ButtonLink, Card, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MATRIX_COLUMN_PAGE_SIZE, MATRIX_PAGE_SIZE } from "@/config";
import { Pages } from "@/features/coverage/Pages";
import { queryFailure } from "@/features/query/QueryInput";
import { t } from "@/i18n";
import { fromMatrixAddress, matrixParams, toMatrixAddress, type MatrixAddress } from "./address";
import { MatrixTable } from "./MatrixTable";
import { MatrixToolbar } from "./MatrixToolbar";

/** A project's traceability matrix, laid out as its address says, a page of rows at a time. */
export function MatrixPage({ project, address, onAddress }: { project: Project; address: MatrixAddress; onAddress: (next: MatrixAddress) => void }) {
  const setup = fromMatrixAddress(address);
  const params = matrixParams(setup);
  const [offset, setOffset] = useState(0);
  const [columnOffset, setColumnOffset] = useState(0);
  const [seen, setSeen] = useState(JSON.stringify(params));
  // Another layout starts again at the first page of rows and of columns.
  if (seen !== JSON.stringify(params)) {
    setSeen(JSON.stringify(params));
    setOffset(0);
    setColumnOffset(0);
  }
  const { data, error, isPlaceholderData, refetch } = useMatrix(project.key, params, offset, columnOffset);
  // A refusal naming the column query belongs under the column filter's box, not the rows'.
  const columnRefused = error instanceof ApiError && error.code === "bad_query" && "columnQuery" in error.fields;
  const { data: types = [] } = useTypes(project.key);
  const { data: statuses = [] } = useStatuses(project.key);
  const { data: attributes = [] } = useAttributes(project.key);
  return (
    <div className="space-y-4" data-matrix={project.key}>
      <PageHeader
        title={t.matrix.title}
        meta={t.matrix.intro}
        actions={
          <div className="flex flex-wrap items-center gap-3">
            <Link
              to="/p/$projectKey/chains"
              params={{ projectKey: project.key }}
              className="text-sm font-medium text-accent hover:underline"
              data-action="open-chains"
            >
              {t.chains.openChains}
            </Link>
            <ButtonLink
              href={matrixHref(project.key, params)}
              download
              variant="secondary"
              size="sm"
              icon={<Icon.Download />}
              title={t.coverage.downloadHint}
              data-action="matrix-csv"
            >
              {t.coverage.download}
            </ButtonLink>
          </div>
        }
      />
      <MatrixToolbar
        project={project}
        setup={setup}
        types={types}
        statuses={statuses}
        attributes={attributes}
        queryError={columnRefused ? undefined : error}
        columnQueryError={columnRefused ? error : undefined}
        onSetup={(next) => onAddress(toMatrixAddress(next))}
      />
      <Card className="space-y-3 p-4">
        {queryFailure(error) ? null : error ? (
          <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
        ) : !data ? (
          <Skeleton />
        ) : data.rows.length === 0 ? (
          <EmptyState icon={<Icon.Check />} title={t.matrix.noRows} />
        ) : data.columns.length === 0 && !params.gaps && !params.columnQuery && !params.columnType ? (
          <EmptyState icon={<Icon.Link />} title={t.coverage.noLinks} />
        ) : (
          <div aria-busy={isPlaceholderData || undefined}>
            <MatrixTable project={project} matrix={data} />
          </div>
        )}
        {data && <Pages offset={offset} shown={data.rows.length} total={data.total} size={MATRIX_PAGE_SIZE} label={t.coverage.matrix} onOffset={setOffset} />}
        {data && (
          <Pages
            offset={columnOffset}
            shown={data.columns.length}
            total={data.columnTotal}
            size={MATRIX_COLUMN_PAGE_SIZE}
            label={t.matrix.columnPages}
            onOffset={setColumnOffset}
          />
        )}
      </Card>
    </div>
  );
}
