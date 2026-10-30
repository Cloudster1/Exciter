import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { useProjectQuality, type QualitySummary } from "@/api/quality";
import { Card, EmptyState, ErrorBanner, PageHeader, SectionTitle, Skeleton, Table, Tag, Td, Th, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { QUALITY_PAGE_SIZE } from "@/config";
import { Pages } from "@/features/coverage/Pages";
import { QueryInput, queryFailure } from "@/features/query/QueryInput";
import { formatNumber } from "@/lib/format";
import { t } from "@/i18n";
import { QualityBadge } from "./QualityBadge";

/** How a project's requirements keep to the writing rules (#207): the scores in bands, the rules broken most, and
 * the requirements lowest first, narrowed by a query as the grid is. */
export function QualityPage({ project }: { project: Project }) {
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useProjectQuality(project.key, query, offset);
  const l = t.quality;
  return (
    <Page width="content" className="space-y-4" data-quality-page={project.key}>
      <PageHeader title={l.pageTitle} meta={l.pageIntro} />
      <QueryInput
        projectKey={project.key}
        value={query}
        name="quality-query"
        label={l.queryLabel}
        error={error}
        onSubmit={(next) => {
          setQuery(next);
          setOffset(0);
        }}
      />
      {error && !queryFailure(error) && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <Card className="space-y-3 p-4">
              <SectionTitle>{l.bandsTitle}</SectionTitle>
              <Totals summary={data} />
              <Bands summary={data} />
            </Card>
            <Card className="space-y-3 p-4">
              <SectionTitle>{l.rulesTitle}</SectionTitle>
              <RulesBroken projectKey={project.key} summary={data} />
            </Card>
          </div>
          <Card className="space-y-3 p-4">
            <SectionTitle>{l.lowestTitle}</SectionTitle>
            {data.requirements.length === 0 ? (
              <EmptyState icon={<Icon.Check />} title={l.noRequirements} />
            ) : (
              <div className="overflow-x-auto">
                <Table aria-label={l.lowestTitle}>
                  <thead>
                    <tr>
                      <Th>{l.columnRequirement}</Th>
                      <Th>{l.columnScore}</Th>
                      <Th>{l.columnFindings}</Th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.requirements.map((row) => (
                      <tr key={row.id} data-quality-row={row.key}>
                        <Td>
                          <Link
                            to="/projects/$projectKey/r/$reqKey"
                            params={{ projectKey: project.key, reqKey: row.key }}
                            className="text-ink hover:text-accent hover:underline"
                          >
                            <span className="font-mono text-xs text-ink-muted">{row.key}</span> {row.title}
                          </Link>
                        </Td>
                        <Td>
                          <QualityBadge score={row.score} stale={row.stale} />
                        </Td>
                        <Td>
                          <span className="block text-sm text-ink-muted">{l.findingCounts(row.active, row.accepted)}</span>
                          <span className="mt-1 flex flex-wrap gap-1">
                            {row.rules.map((rule) => (
                              <Tag key={rule} data-rule={rule}>
                                {l.rules[rule].name}
                              </Tag>
                            ))}
                          </span>
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
            )}
            <Pages offset={offset} shown={data.requirements.length} total={data.total} size={QUALITY_PAGE_SIZE} label={l.lowestTitle} onOffset={setOffset} />
          </Card>
        </>
      )}
    </Page>
  );
}

function Totals({ summary }: { summary: QualitySummary }) {
  const l = t.quality;
  const items = [
    { label: l.average, value: summary.average === null ? l.noScore : formatNumber(summary.average, { maximumFractionDigits: 0 }), name: "average" },
    { label: l.checkedCount, value: formatNumber(summary.checked), name: "checked" },
    { label: l.uncheckedCount, value: formatNumber(summary.unchecked), name: "unchecked" },
  ];
  return (
    <dl className="grid grid-cols-3 gap-2">
      {items.map((item) => (
        <div key={item.name} data-quality-total={item.name}>
          <dt className="text-xs text-ink-muted">{item.label}</dt>
          <dd className="text-xl font-semibold text-ink tabular-nums">{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** The bands as columns, one hue; each says its range and count in words, which is what a screen reader reads. */
function Bands({ summary }: { summary: QualitySummary }) {
  const l = t.quality;
  const most = Math.max(0, ...summary.bands.map((band) => band.count));
  return (
    <ul aria-label={l.bandsChart} className="flex h-40 items-end gap-2 border-b border-border" data-quality-bands>
      {summary.bands.map((band) => {
        const share = most === 0 ? 0 : (band.count / most) * 100;
        return (
          <li key={band.from} className="flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-1" data-band={`${band.from}-${band.to}`}>
            <span className="sr-only">{l.band(band.from, band.to, band.count)}</span>
            <span aria-hidden="true" className="text-xs text-ink tabular-nums">
              {formatNumber(band.count)}
            </span>
            <span aria-hidden="true" className="w-full rounded-t bg-chart-1" style={{ height: `${share}%` }} title={l.band(band.from, band.to, band.count)} />
            <span aria-hidden="true" className="pb-1 text-2xs whitespace-nowrap text-ink-muted tabular-nums">
              {l.bandRange(band.from, band.to)}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

function RulesBroken({ projectKey, summary }: { projectKey: string; summary: QualitySummary }) {
  const l = t.quality;
  const rules = summary.rules.filter((each) => each.count > 0).sort((a, b) => b.count - a.count);
  if (rules.length === 0) return <p className="text-sm text-ink-subtle">{l.noRulesBroken}</p>;
  const most = rules[0]!.count;
  return (
    <ul className="space-y-2" aria-label={l.rulesTitle}>
      {rules.map((each) => (
        <li key={each.rule} className="space-y-1" data-rule-count={each.rule}>
          <div className="flex items-baseline justify-between gap-2 text-sm">
            <Link
              to="/projects/$projectKey/grid"
              params={{ projectKey }}
              search={{ query: `qualityFinding = ${each.rule}` }}
              className="font-medium text-ink hover:text-accent hover:underline"
            >
              {l.rules[each.rule].name}
            </Link>
            <span className="text-ink-muted tabular-nums">{l.ruleCount(each.count)}</span>
          </div>
          <div className="flex h-2 overflow-hidden rounded-full bg-surface-sunken" aria-hidden="true">
            <span className="rounded-full bg-chart-1" style={{ width: `${(each.count / most) * 100}%` }} />
          </div>
        </li>
      ))}
    </ul>
  );
}
