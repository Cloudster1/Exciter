import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { useRequirements } from "@/api/requirements";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REQUIREMENT_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const changedAt = localDateFormat({ dateStyle: "medium" });

/** A project's requirements by key, a page at a time; the tree (#15) and the grid (#20) come later. */
export function RequirementList({ project }: { project: Project }) {
  const navigate = useNavigate();
  const [offset, setOffset] = useState(0);
  const { data, error, isLoading, refetch } = useRequirements(project.key, { limit: REQUIREMENT_PAGE_SIZE, offset });
  const mayWrite = project.can.edit;
  const create = () => navigate({ to: "/p/$projectKey/new", params: { projectKey: project.key } });
  return (
    <div className="mx-auto max-w-5xl" data-requirement-list={project.key}>
      <PageHeader
        title={t.requirements.title}
        meta={t.projects.projectAndKey(project.name, project.key)}
        actions={
          mayWrite && (
            <Button icon={<Icon.Plus />} onClick={create} data-action="new-requirement">
              {t.requirements.create}
            </Button>
          )
        }
      />
      {project.archivedAt && (
        <p role="status" className="mb-4 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-archived-notice>
          {t.projects.archivedNotice}
        </p>
      )}
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {data && data.total === 0 && (
        <EmptyState
          icon={<Icon.Folder />}
          title={t.requirements.emptyTitle}
          description={mayWrite ? t.requirements.emptyBody : t.requirements.emptyBodyReader}
          action={
            mayWrite && (
              <Button icon={<Icon.Plus />} onClick={create} data-action="first-requirement">
                {t.requirements.create}
              </Button>
            )
          }
        />
      )}
      {data && data.total > 0 && (
        <>
          <Table aria-label={t.requirements.tableLabel}>
            <thead>
              <tr>
                <Th className="w-28">{t.requirements.columnKey}</Th>
                <Th>{t.requirements.columnTitle}</Th>
                <Th className="w-40">{t.requirements.columnOwner}</Th>
                <Th className="w-24">{t.requirements.columnPriority}</Th>
                <Th className="w-32">{t.requirements.columnUpdated}</Th>
              </tr>
            </thead>
            <tbody>
              {data.rows.map((row) => (
                <tr key={row.id} data-requirement-row={row.key}>
                  <Td>
                    <span className="font-mono text-xs text-ink-muted">{row.key}</span>
                  </Td>
                  <Td>
                    <Link
                      to="/p/$projectKey/r/$reqKey"
                      params={{ projectKey: project.key, reqKey: row.key }}
                      className="font-medium text-ink hover:text-accent hover:underline"
                    >
                      {row.title}
                    </Link>
                    {row.kind === "section" && <Tag className="ml-2 align-middle">{t.requirements.kinds.section}</Tag>}
                  </Td>
                  <Td>
                    <span className="text-ink-muted">{row.owner?.name ?? ""}</span>
                  </Td>
                  <Td>
                    <span className="text-ink-muted">{row.priority ? t.requirements.priorities[row.priority] : ""}</span>
                  </Td>
                  <Td>
                    <span className="text-ink-muted">{changedAt.format(new Date(row.updatedAt))}</span>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
          {data.total > REQUIREMENT_PAGE_SIZE && (
            <div className="mt-3 flex items-center justify-end gap-2 text-sm text-ink-muted" data-requirement-pages>
              <span>{t.requirements.range(offset + 1, Math.min(offset + REQUIREMENT_PAGE_SIZE, data.total), data.total)}</span>
              <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - REQUIREMENT_PAGE_SIZE))}>
                {t.requirements.previous}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={offset + REQUIREMENT_PAGE_SIZE >= data.total}
                onClick={() => setOffset(offset + REQUIREMENT_PAGE_SIZE)}
              >
                {t.requirements.next}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
