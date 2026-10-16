import { useState } from "react";
import { useAttributes, useTypes } from "@/api/attributes";
import { matrixHref, useMatrix } from "@/api/coverage";
import type { Project } from "@/api/projects";
import { useStatuses } from "@/api/workflows";
import { ButtonLink, Card, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MATRIX_PAGE_SIZE } from "@/config";
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
  const [seen, setSeen] = useState(JSON.stringify(params));
  // Another layout starts again at the first page.
  if (seen !== JSON.stringify(params)) {
    setSeen(JSON.stringify(params));
    setOffset(0);
  }
  const { data, error, isPlaceholderData, refetch } = useMatrix(project.key, params, offset);
  const { data: types = [] } = useTypes(project.key);
  const { data: statuses = [] } = useStatuses(project.key);
  const { data: attributes = [] } = useAttributes(project.key);
  return (
    <div className="space-y-4" data-matrix={project.key}>
      <PageHeader
        title={t.matrix.title}
        meta={t.matrix.intro}
        actions={
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
        }
      />
      <MatrixToolbar
        project={project}
        setup={setup}
        types={types}
        statuses={statuses}
        attributes={attributes}
        queryError={error}
        onSetup={(next) => onAddress(toMatrixAddress(next))}
      />
      <Card className="space-y-3 p-4">
        {queryFailure(error) ? null : error ? (
          <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
        ) : !data ? (
          <Skeleton />
        ) : data.columns.length === 0 ? (
          <EmptyState icon={<Icon.Link />} title={t.coverage.noLinks} />
        ) : (
          <div aria-busy={isPlaceholderData || undefined}>
            <MatrixTable project={project} matrix={data} />
          </div>
        )}
        {data && <Pages offset={offset} shown={data.rows.length} total={data.total} size={MATRIX_PAGE_SIZE} label={t.coverage.matrix} onOffset={setOffset} />}
      </Card>
    </div>
  );
}
