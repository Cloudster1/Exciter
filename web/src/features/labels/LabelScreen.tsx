import { Link } from "@tanstack/react-router";
import { useLabelledRequirements, type LabelledRequirement } from "@/api/labels";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LABEL_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { LabelLink } from "./RequirementLabels";

const changedOn = localDateFormat({ dateStyle: "medium" });

/** The requirements that carry a label, in one project or in every one, a page of the list at a time. */
export function LabelScreen({ name, projectKey, page, onPage }: { name: string; projectKey?: string; page: number; onPage: (page: number) => void }) {
  const { data, error, isLoading, isPlaceholderData, refetch } = useLabelledRequirements(name, projectKey, (page - 1) * LABEL_PAGE_SIZE);
  const pages = data ? Math.max(1, Math.ceil(data.total / LABEL_PAGE_SIZE)) : 1;
  const rows = data?.requirements ?? [];

  return (
    <div className="mx-auto max-w-4xl" data-label-screen={name}>
      <PageHeader
        title={
          <span className="inline-flex items-center gap-2">
            <Icon.Label />
            <span data-label-heading>{name}</span>
          </span>
        }
        meta={
          <span className="flex flex-wrap items-center gap-3">
            {projectKey ? t.labels.inProject(projectKey.toUpperCase()) : t.labels.everywhere}
            {projectKey && (
              <Link to="/labels/$name" params={{ name }} search={{}} className="text-accent hover:underline" data-action="label-everywhere">
                {t.labels.showEverywhere}
              </Link>
            )}
          </span>
        }
      />
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message || t.labels.failed}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton rows={5} />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Label />} title={t.labels.emptyTitle} description={t.labels.emptyBody} />
      ) : (
        <>
          <p className="mb-2 text-sm text-ink-subtle tabular-nums" role="status" data-label-count>
            {t.labels.count(data.total, data.offset + 1, data.offset + rows.length)}
          </p>
          <ol className="divide-y divide-border" aria-label={t.labels.heading(name)} aria-busy={isPlaceholderData || undefined}>
            {rows.map((each) => (
              <Row key={each.id} requirement={each} current={name} />
            ))}
          </ol>
          {pages > 1 && (
            <nav aria-label={t.labels.paging} className="mt-4 flex items-center justify-between gap-2" data-label-paging>
              <Button variant="secondary" size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)} data-action="previous-labelled">
                {t.labels.previous}
              </Button>
              <span className="text-sm text-ink-subtle tabular-nums">{t.labels.pageOf(page, pages)}</span>
              <Button variant="secondary" size="sm" disabled={page >= pages} onClick={() => onPage(page + 1)} data-action="next-labelled">
                {t.labels.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </div>
  );
}

function Row({ requirement, current }: { requirement: LabelledRequirement; current: string }) {
  const others = requirement.labels.filter((each) => each !== current);
  return (
    <li className="py-3" data-labelled-requirement={requirement.key}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs text-ink-muted">{requirement.key}</span>
        <Link
          to="/p/$projectKey/r/$reqKey"
          params={{ projectKey: requirement.projectKey, reqKey: requirement.key }}
          className="font-medium text-accent hover:underline"
        >
          {requirement.title}
        </Link>
        {requirement.kind === "section" && <Tag>{t.requirements.kinds.section}</Tag>}
      </div>
      <p className="mt-0.5 text-xs text-ink-subtle">
        {requirement.projectName}
        {" / "}
        {t.labels.changed(requirement.updatedByName, changedOn.format(new Date(requirement.updatedAt)))}
      </p>
      {others.length > 0 && (
        <ul className="mt-1.5 flex flex-wrap gap-1.5" aria-label={t.labels.title}>
          {others.map((name) => (
            <li key={name}>
              <LabelLink name={name} />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}
