import { Link } from "@tanstack/react-router";
import { NOW, useBaselineComparison, type BaselineComparison, type ItemChange } from "@/api/baselines";
import type { Project } from "@/api/projects";
import { Card, EmptyState, ErrorBanner, PageHeader, SectionTitle, Skeleton, Tag, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const at = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

function sideName(side: BaselineComparison["from"]): string {
  return side.baseline ? side.baseline.name : `${t.baseline.now} (${at.format(new Date(side.at))})`;
}

/** What differs from a baseline to another or to now: requirements added, removed and changed, and links. */
export function BaselineCompare({ project, baselineId, other }: { project: Project; baselineId: string; other: string }) {
  const { data, error, refetch } = useBaselineComparison(baselineId, other || NOW);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  const nothing = data.items.length === 0 && data.links.length === 0 && data.externalLinks.length === 0;
  return (
    <Page width="content" className="space-y-4" data-baseline-comparison="">
      <PageHeader
        crumb={
          <Link to="/p/$projectKey/baselines/$baselineId" params={{ projectKey: project.key, baselineId }} className="hover:text-accent hover:underline">
            {data.from.baseline?.name ?? t.baseline.title}
          </Link>
        }
        title={t.baseline.compareTitle(sideName(data.from), sideName(data.to))}
      />
      {nothing && <EmptyState icon={<Icon.Check />} title={t.baseline.nothingDiffers} />}
      {data.items.length > 0 && (
        <Card className="space-y-2 p-4">
          <SectionTitle>{t.baseline.requirements}</SectionTitle>
          <ul className="flex flex-col divide-y divide-border" aria-label={t.baseline.requirements}>
            {data.items.map((item) => (
              <Item key={item.key} project={project} item={item} />
            ))}
          </ul>
        </Card>
      )}
      {data.links.length > 0 && (
        <Card className="space-y-2 p-4">
          <SectionTitle>{t.baseline.links}</SectionTitle>
          <ul className="space-y-1 text-sm" aria-label={t.baseline.links}>
            {data.links.map((l) => (
              <li key={`${l.change} ${l.sourceKey} ${l.typeName} ${l.targetKey}`} className="flex items-center gap-2" data-link-change={l.change}>
                <Tag>{t.baseline.changes[l.change]}</Tag>
                <span className="font-mono text-xs">{t.baseline.linkRow(l.sourceKey, l.typeName.toLowerCase(), l.targetKey)}</span>
              </li>
            ))}
          </ul>
        </Card>
      )}
      {data.externalLinks.length > 0 && (
        <Card className="space-y-2 p-4">
          <SectionTitle>{t.baseline.externalLinks}</SectionTitle>
          <ul className="space-y-1 text-sm" aria-label={t.baseline.externalLinks}>
            {data.externalLinks.map((l) => (
              <li key={`${l.change} ${l.requirementKey} ${l.relation} ${l.issueKey}`} className="flex items-center gap-2" data-external-change={l.change}>
                <Tag>{t.baseline.changes[l.change]}</Tag>
                <span className="font-mono text-xs">{t.baseline.externalRow(l.requirementKey, l.relation, l.issueKey)}</span>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </Page>
  );
}

function Item({ project, item }: { project: Project; item: ItemChange }) {
  const { from, to } = item;
  const title = to?.title || from?.title || t.baseline.purged;
  return (
    <li className="flex flex-col gap-1 py-2" data-item-change={item.key} data-change={item.change}>
      <span className="flex flex-wrap items-center gap-2 text-sm">
        <Tag>{t.baseline.changes[item.change]}</Tag>
        <span className="font-mono text-xs text-ink-muted">{to?.outline || from?.outline}</span>
        <span className="font-medium text-ink">{title}</span>
        <span className="font-mono text-xs text-ink-subtle">{item.key}</span>
      </span>
      {item.change === "changed" && from && to && (
        <span className="flex flex-wrap items-center gap-x-3 text-xs text-ink-muted">
          {item.revised && (
            <>
              <span data-revised="">{t.baseline.revised(from.revision, to.revision)}</span>
              <Link
                to="/p/$projectKey/r/$reqKey/history"
                params={{ projectKey: project.key, reqKey: item.key }}
                search={{ from: from.revision, to: to.revision }}
                className="text-accent hover:underline"
              >
                {t.baseline.showChange}
              </Link>
            </>
          )}
          {item.moved && <span data-moved="">{t.baseline.moved(from.outline, to.outline)}</span>}
          {item.statusChanged && <span data-status-changed="">{t.baseline.statusChanged(from.status?.name ?? "", to.status?.name ?? "")}</span>}
        </span>
      )}
    </li>
  );
}
