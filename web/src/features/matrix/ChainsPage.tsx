import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useAttributes, useTypes } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { useStatorPages } from "@/api/stator";
import { chainsHref, useChains, useTraceModel, type TraceChainNode, type TraceChains } from "@/api/trace";
import { useStatuses } from "@/api/workflows";
import { ButtonLink, Card, Checkbox, EmptyState, ErrorBanner, PageHeader, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { CHAIN_PAGE_SIZE } from "@/config";
import { Pages } from "@/features/coverage/Pages";
import { GridFilters } from "@/features/grid/GridFilters";
import { queryFailure } from "@/features/query/QueryInput";
import { t } from "@/i18n";
import { GridViews } from "@/features/grid/GridViews";
import { chainParams, chainSettings, fromChainView, fromChainsAddress, levelRange, toChainsAddress, type ChainsAddress } from "./chainsAddress";

/** A project's chains (#151): every path from the trace model's first level shown down to its last, a page of roots
 * at a time, each line marked where it breaks. */
export function ChainsPage({ project, address, onAddress }: { project: Project; address: ChainsAddress; onAddress: (next: ChainsAddress) => void }) {
  const setup = fromChainsAddress(address);
  const params = chainParams(setup);
  const [offset, setOffset] = useState(0);
  const [seen, setSeen] = useState(JSON.stringify(params));
  // Another filter or other levels start again at the first page.
  if (seen !== JSON.stringify(params)) {
    setSeen(JSON.stringify(params));
    setOffset(0);
  }
  const { data: model } = useTraceModel(project.key);
  const { data, error, isPlaceholderData, refetch } = useChains(project.key, params, offset);
  const { data: types = [] } = useTypes(project.key);
  const { data: statuses = [] } = useStatuses(project.key);
  const { data: attributes = [] } = useAttributes(project.key);
  const levels = model?.levels ?? [];
  const from = setup.levels.length ? Math.min(...setup.levels) : 0;
  const to = setup.levels.length ? Math.max(...setup.levels) : levels.length - 1;
  const set = (patch: Partial<typeof setup>) => onAddress(toChainsAddress({ ...setup, ...patch }, address.view));
  const [notice, setNotice] = useState("");
  return (
    <div className="space-y-4" data-chains={project.key}>
      <PageHeader
        title={t.chains.title}
        meta={t.chains.intro}
        actions={
          <div className="flex flex-wrap items-center gap-3">
            <Link
              to="/p/$projectKey/matrix"
              params={{ projectKey: project.key }}
              className="text-sm font-medium text-accent hover:underline"
              data-action="open-matrix"
            >
              {t.chains.openMatrix}
            </Link>
            <ButtonLink href={chainsHref(project.key, params)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="chains-csv">
              {t.coverage.download}
            </ButtonLink>
            <ButtonLink
              href={chainsHref(project.key, params, "xlsx")}
              download
              variant="secondary"
              size="sm"
              icon={<Icon.Download />}
              data-action="chains-xlsx"
            >
              {t.coverage.downloadExcel}
            </ButtonLink>
          </div>
        }
      />
      <div className="flex flex-wrap items-center gap-3">
        <GridViews
          project={project}
          kind="chain"
          viewId={address.view}
          state={{ filter: setup.filter, sort: setup.sort, settings: chainSettings(setup) }}
          onOpen={(view) => {
            onAddress(view ? fromChainView(view) : {});
            // Saving says so before it opens the view saved, which has lost nothing to say.
            if (view?.dropped.length) setNotice(t.matrix.dropped(view.dropped.length));
          }}
          onNotice={setNotice}
        />
        <p role="status" className="text-sm text-ink-muted empty:hidden" data-chains-notice>
          {notice}
        </p>
      </div>
      <GridFilters
        project={project}
        filter={setup.filter}
        types={types}
        statuses={statuses}
        attributes={attributes}
        queryError={error}
        onFilter={(filter) => set({ filter })}
      />
      {model && levels.length === 0 ? (
        <EmptyState
          icon={<Icon.Link />}
          title={t.chains.noModelReport}
          action={
            project.can.administer ? (
              <Link
                to="/p/$projectKey/settings"
                params={{ projectKey: project.key }}
                search={{ tab: "trace" }}
                className="text-sm font-medium text-accent hover:underline"
              >
                {t.chains.setModel}
              </Link>
            ) : undefined
          }
        />
      ) : (
        <>
          {levels.length > 1 && (
            <Card className="flex flex-wrap items-end gap-4 p-3" data-chains-toolbar>
              <div className="min-w-40">
                <Select
                  label={t.chains.from}
                  value={from}
                  onChange={(e) => set({ levels: levelRange(Number(e.target.value), to, levels.length) })}
                  controlSize="sm"
                >
                  {levels.map((level, i) => (
                    <option key={level.name} value={i} disabled={i > to || level.kind !== "requirements"}>
                      {level.name}
                    </option>
                  ))}
                </Select>
              </div>
              <div className="min-w-40">
                <Select
                  label={t.chains.to}
                  value={to}
                  onChange={(e) => set({ levels: levelRange(from, Number(e.target.value), levels.length) })}
                  controlSize="sm"
                >
                  {levels.map((level, i) => (
                    <option key={level.name} value={i} disabled={i < from}>
                      {level.name}
                    </option>
                  ))}
                </Select>
              </div>
              <Checkbox label={t.chains.onlyBroken} checked={setup.onlyBroken} onChange={(e) => set({ onlyBroken: e.target.checked })} />
            </Card>
          )}
          <Card className="space-y-3 p-4">
            {queryFailure(error) ? null : error ? (
              <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
            ) : !data ? (
              <Skeleton />
            ) : data.lines.length === 0 ? (
              <EmptyState icon={<Icon.Check />} title={t.chains.noLines} />
            ) : (
              <div aria-busy={isPlaceholderData || undefined}>
                <p className="mb-2 text-sm text-ink-subtle" role="status">
                  {t.chains.counts(data.lines.length, data.broken)}
                </p>
                <ChainTable project={project} chains={data} />
              </div>
            )}
            {data && <Pages offset={offset} shown={data.roots.length} total={data.total} size={CHAIN_PAGE_SIZE} label={t.chains.roots} onOffset={setOffset} />}
          </Card>
        </>
      )}
    </div>
  );
}

/** The lines of a page of chains, a column per level and a break marked where a line stops short. */
function ChainTable({ project, chains }: { project: Project; chains: TraceChains }) {
  const pageIds = chains.lines.flatMap((line) => line.nodes.flatMap((n) => (n.page ? [n.page.id] : [])));
  const lookup = useStatorPages(pageIds, pageIds.length > 0);
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm" aria-label={t.chains.roots} data-chain-table>
        <thead>
          <tr>
            {chains.levels.map((level) => (
              <th key={level.name} scope="col" className="border-b border-border px-2 py-1 text-left font-medium text-ink-muted">
                {level.name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {chains.lines.map((line, i) => (
            <tr key={i} className="border-t border-border" data-chain-line={i} data-broken={line.broken || undefined}>
              {chains.levels.map((level, at) => {
                const node = line.nodes[at];
                if (node) {
                  return (
                    <td key={level.name} className="px-2 py-1 align-top">
                      <Node project={project} node={node} title={node.page ? lookup.pages.get(node.page.id)?.title : undefined} />
                    </td>
                  );
                }
                return (
                  <td key={level.name} className="px-2 py-1 align-top">
                    {line.broken && at === line.nodes.length && (
                      <Tag className="bg-danger-subtle text-danger" data-break="">
                        {t.chains.breaks}
                      </Tag>
                    )}
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

/** What a line holds at one level, and the link that led there, marked when suspect. */
function Node({ project, node, title }: { project: Project; node: TraceChainNode; title?: string }) {
  return (
    <span className="inline-flex flex-wrap items-baseline gap-1.5">
      {node.via && <span className="text-xs text-ink-subtle">{node.via}</span>}
      {node.requirement ? (
        <Link
          to="/p/$projectKey/r/$reqKey"
          params={{ projectKey: project.key, reqKey: node.requirement.key }}
          className="text-ink hover:text-accent hover:underline"
        >
          <span className="font-mono text-xs text-ink-muted">{node.requirement.key}</span> {node.requirement.title}
        </Link>
      ) : node.page ? (
        <a href={node.page.url} target="_blank" rel="noopener noreferrer" className="text-ink hover:text-accent hover:underline">
          {title ?? `${node.page.spaceKey}/${node.page.id}`}
        </a>
      ) : node.code ? (
        <span className="font-mono text-xs text-ink" data-chain-code={`${node.code.path}:${node.code.line}`}>
          {node.code.url ? (
            <a href={node.code.url} target="_blank" rel="noopener noreferrer" className="hover:text-accent hover:underline">
              {node.code.path}:{node.code.line}
            </a>
          ) : (
            `${node.code.path}:${node.code.line}`
          )}
        </span>
      ) : node.test ? (
        <span className="text-ink" data-chain-test={node.test.name}>
          {node.test.name} <Tag>{t.evidence.results.outcome[node.test.outcome] ?? node.test.outcome}</Tag>
        </span>
      ) : (
        <span className="font-mono text-xs text-ink">{node.issueKey}</span>
      )}
      {node.test?.outdated && <Icon.Warning className="size-3.5 text-warning" label={t.evidence.matrix.outdated} />}
      {node.code?.outdated && <Icon.Warning className="size-3.5 text-warning" label={t.evidence.matrix.outdated} />}
      {node.suspect && !node.test && !node.code && <Icon.Warning className="size-3.5 text-warning" label={t.coverage.suspect} />}
    </span>
  );
}
