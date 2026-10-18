import { useCallback, useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useArmatureIssues } from "@/api/armature";
import { useProjectLinkTypes } from "@/api/links";
import type { Project } from "@/api/projects";
import type { Requirement } from "@/api/requirements";
import { useStatorPages } from "@/api/stator";
import { GRAPH_RELATIONS, graphHref, useTraceGraph, type TraceGraph, type TraceGraphNode } from "@/api/trace";
import { Icon } from "@/components/icons";
import { ButtonLink, Card, Checkbox, WordButton, ErrorBanner, PageHeader, Segmented, Select, Skeleton } from "@/components/ui";
import { GRAPH_DEFAULT_DEPTH, GRAPH_MAX_DEPTH } from "@/config";
import { t } from "@/i18n";
import { graphParams, type GraphAddress } from "./address";
import { GraphCanvas } from "./GraphCanvas";
import { nodeName, reachedFrom } from "./layout";
import { NodeCard, nodeMarks } from "./NodeCard";

const DEPTHS = Array.from({ length: GRAPH_MAX_DEPTH }, (_, i) => i + 1);

/** A requirement's trace graph (#211): everything it traces to, up and down, drawn or listed, with the impact of
 * changing it beside. */
export function TraceGraphPage({
  project,
  requirement,
  address,
  onAddress,
}: {
  project: Project;
  requirement: Requirement;
  address: GraphAddress;
  onAddress: (next: GraphAddress) => void;
}) {
  const params = graphParams(address);
  const { data: graph, error, refetch } = useTraceGraph(requirement.key, params);
  const { data: linkTypes = [] } = useProjectLinkTypes(project.key);
  const [chosen, setChosen] = useState<string>();
  const navigate = useNavigate();
  const issueKeys = useMemo(() => graph?.nodes.flatMap((n) => (n.issueKey ? [n.issueKey] : [])) ?? [], [graph]);
  const pageIds = useMemo(() => graph?.nodes.flatMap((n) => (n.page ? [n.page.id] : [])) ?? [], [graph]);
  const issues = useArmatureIssues(issueKeys, issueKeys.length > 0);
  const pages = useStatorPages(pageIds, pageIds.length > 0);
  const titles = useMemo(() => {
    const out = new Map<string, string>();
    for (const node of graph?.nodes ?? []) {
      if (node.issueKey && issues.issues.has(node.issueKey)) out.set(node.id, issues.issues.get(node.issueKey)?.summary ?? t.graph.unseenIssue);
      if (node.page) {
        const page = pages.pages.get(node.page.id);
        if (page) out.set(node.id, page.title);
      }
    }
    return out;
  }, [graph, issues, pages]);
  const labelOf = useCallback(
    (node: TraceGraphNode) => {
      const marks = nodeMarks(node).map((m) => m.label);
      return [t.graph.kinds[node.kind], nodeName(node), node.requirement?.title ?? titles.get(node.id) ?? "", ...marks].filter(Boolean).join(", ");
    },
    [titles],
  );
  const centre = useCallback(
    (node: TraceGraphNode) => {
      if (!node.requirement) return;
      setChosen(undefined);
      void navigate({
        to: "/p/$projectKey/r/$reqKey/trace",
        params: { projectKey: node.requirement.projectKey, reqKey: node.requirement.key },
        search: address,
      });
    },
    [navigate, address],
  );
  const typeKeys = linkTypes.filter((lt) => lt.enabled && !lt.retired).map((lt) => lt.key);

  return (
    <div className="space-y-4" data-trace-graph={requirement.key}>
      <PageHeader
        crumb={
          <Link
            to="/p/$projectKey/r/$reqKey"
            params={{ projectKey: project.key, reqKey: requirement.key }}
            className="hover:text-accent hover:underline"
            data-action="back-to-requirement"
          >
            {t.graph.back}
          </Link>
        }
        title={t.graph.title(requirement.key)}
        meta={
          <span>
            {requirement.title}. {t.graph.intro}
          </span>
        }
        actions={
          <ButtonLink href={graphHref(requirement.key, params)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="graph-csv">
            {t.graph.download}
          </ButtonLink>
        }
      />
      <div className="flex flex-wrap items-end gap-3" data-graph-controls>
        <div className="w-40">
          <Select
            label={t.graph.direction}
            value={address.direction ?? "both"}
            onChange={(event) => onAddress({ ...address, direction: event.target.value === "both" ? undefined : (event.target.value as "up" | "down") })}
            controlSize="sm"
          >
            {(["both", "up", "down"] as const).map((d) => (
              <option key={d} value={d}>
                {t.graph.directions[d]}
              </option>
            ))}
          </Select>
        </div>
        <div className="w-32">
          <Select
            label={t.graph.depth}
            value={address.depth ?? GRAPH_DEFAULT_DEPTH}
            onChange={(event) => {
              const depth = Number(event.target.value);
              onAddress({ ...address, depth: depth === GRAPH_DEFAULT_DEPTH ? undefined : depth });
            }}
            controlSize="sm"
          >
            {DEPTHS.map((d) => (
              <option key={d} value={d}>
                {t.graph.levels(d)}
              </option>
            ))}
          </Select>
        </div>
        <Choice
          legend={t.graph.types}
          all={typeKeys}
          names={Object.fromEntries(linkTypes.map((lt) => [lt.key, lt.name]))}
          chosen={address.type}
          onChange={(type) => onAddress({ ...address, type })}
          data-graph-types=""
        />
        <Choice
          legend={t.graph.relations}
          all={[...GRAPH_RELATIONS]}
          names={t.graph.relationNames}
          chosen={address.relation}
          onChange={(relation) => onAddress({ ...address, relation })}
          data-graph-relations=""
        />
        <Segmented
          label={t.graph.view}
          value={address.view ?? "graph"}
          options={[
            { value: "graph", label: t.graph.views.graph },
            { value: "list", label: t.graph.views.list },
          ]}
          onChange={(view) => onAddress({ ...address, view: view === "list" ? "list" : undefined })}
          size="sm"
        />
      </div>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!graph && !error && <Skeleton />}
      {graph && (
        <>
          {graph.truncated && (
            <p role="status" className="rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink" data-graph-truncated>
              {t.graph.truncated(graph.nodes.length)}
            </p>
          )}
          {graph.hidden > 0 && (
            <p className="text-sm text-ink-muted" data-graph-hidden={graph.hidden}>
              {t.graph.hidden(graph.hidden)}
            </p>
          )}
          {graph.nodes.length === 1 && <p className="text-sm text-ink-muted">{t.graph.nothing}</p>}
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <div className="min-w-0">
              {address.view === "list" ? (
                <GraphList graph={graph} titles={titles} onCentre={centre} />
              ) : (
                <GraphCanvas graph={graph} reqKey={requirement.key} titles={titles} labelOf={labelOf} chosen={chosen} onChoose={setChosen} onCentre={centre} />
              )}
            </div>
            <div className="space-y-4">
              {address.view !== "list" && (
                <Chosen graph={graph} id={chosen} title={chosen ? titles.get(chosen) : undefined} issueUrl={issueUrl(issues.issues)} onCentre={centre} />
              )}
              <ImpactList graph={graph} titles={titles} />
            </div>
          </div>
        </>
      )}
    </div>
  );
}

/** Where an issue opens in Armature, when the lookup said. */
function issueUrl(issues: Map<string, { url: string } | null>) {
  return (key: string) => issues.get(key)?.url;
}

/** A choice among several, every one when none is named: unchecking narrows, and checking the last again widens. */
function Choice({
  legend,
  all,
  names,
  chosen,
  onChange,
  ...rest
}: {
  legend: string;
  all: string[];
  names: Record<string, string>;
  chosen?: string[];
  onChange: (next?: string[]) => void;
} & Record<`data-${string}`, string>) {
  const on = new Set(chosen ?? all);
  const toggle = (key: string, checked: boolean) => {
    const next = all.filter((k) => (k === key ? checked : on.has(k)));
    onChange(next.length === all.length || next.length === 0 ? undefined : next);
  };
  return (
    <details className="relative" {...rest}>
      <summary className="cursor-pointer rounded-control border border-border bg-surface px-3 py-1.5 text-sm text-ink">
        {legend} ({chosen ? chosen.length : all.length}/{all.length})
      </summary>
      <fieldset className="absolute z-10 mt-1 w-64 space-y-1 rounded-control border border-border bg-surface-raised p-3 shadow-2">
        <legend className="sr-only">{legend}</legend>
        {all.map((key) => (
          <Checkbox key={key} label={names[key] ?? key} checked={on.has(key)} onChange={(event) => toggle(key, event.target.checked)} />
        ))}
      </fieldset>
    </details>
  );
}

/** The node chosen on the canvas, said in full, with the ways on from it. */
function Chosen({
  graph,
  id,
  title,
  issueUrl,
  onCentre,
}: {
  graph: TraceGraph;
  id?: string;
  title?: string;
  issueUrl: (key: string) => string | undefined;
  onCentre: (node: TraceGraphNode) => void;
}) {
  const node = graph.nodes.find((n) => n.id === id);
  const reached = useMemo(() => reachedFrom(graph), [graph]);
  return (
    <Card className="space-y-3 p-4" data-graph-chosen={node ? nodeName(node) : ""}>
      <h2 className="text-sm font-semibold text-ink">{t.graph.chosen}</h2>
      <div aria-live="polite">
        {!node ? (
          <p className="text-sm text-ink-muted">{t.graph.choose}</p>
        ) : (
          <div className="space-y-3">
            <NodeCard node={node} title={title} suspect={reached.get(node.id)?.edge.suspect} />
            <ReachedBy reached={reached.get(node.id)} />
            <div className="flex flex-wrap gap-2">
              {node.requirement && node.level !== 0 && (
                <WordButton className="text-sm font-medium text-accent hover:underline" onClick={() => onCentre(node)} data-action="graph-centre">
                  {t.graph.centre}
                </WordButton>
              )}
              <OpenLink node={node} issueUrl={issueUrl} />
            </div>
          </div>
        )}
      </div>
    </Card>
  );
}

function ReachedBy({ reached }: { reached?: { from: TraceGraphNode; edge: { typeKey?: string; relation?: string; phrase?: string } } }) {
  if (!reached) return null;
  const via = reached.edge.phrase || reached.edge.typeKey || reached.edge.relation || "";
  return <p className="text-xs text-ink-muted">{t.graph.reachedBy(via, nodeName(reached.from))}</p>;
}

/** Where a node opens: a requirement in its own project, an issue in Armature, a page in Stator, a place in the code. */
function OpenLink({ node, issueUrl }: { node: TraceGraphNode; issueUrl: (key: string) => string | undefined }) {
  const external = (href: string | undefined, children: ReactNode) =>
    href ? (
      <a href={href} target="_blank" rel="noopener noreferrer" className="text-sm font-medium text-accent hover:underline" data-action="graph-open">
        {children}
      </a>
    ) : null;
  if (node.requirement) {
    return (
      <Link
        to="/p/$projectKey/r/$reqKey"
        params={{ projectKey: node.requirement.projectKey, reqKey: node.requirement.key }}
        className="text-sm font-medium text-accent hover:underline"
        data-action="graph-open"
      >
        {t.graph.open}
      </Link>
    );
  }
  if (node.issueKey) return external(issueUrl(node.issueKey), t.graph.open);
  if (node.page) return external(node.page.url, t.graph.open);
  if (node.code) return external(node.code.url || undefined, t.graph.open);
  return null;
}

/** The graph as nested lists, each node below the one it was reached from: the picture's twin for a screen reader. */
function GraphList({ graph, titles, onCentre }: { graph: TraceGraph; titles: Map<string, string>; onCentre: (node: TraceGraphNode) => void }) {
  const reached = useMemo(() => reachedFrom(graph), [graph]);
  const children = useMemo(() => {
    const out = new Map<string, TraceGraphNode[]>();
    for (const node of graph.nodes) {
      const from = reached.get(node.id)?.from.id;
      if (from) out.set(from, [...(out.get(from) ?? []), node]);
    }
    return out;
  }, [graph, reached]);
  const root = graph.nodes.find((n) => n.id === graph.root);
  if (!root) return null;
  const item = (node: TraceGraphNode): ReactNode => {
    const below = children.get(node.id) ?? [];
    const up = below.filter((n) => n.level < 0);
    const down = below.filter((n) => n.level > 0);
    return (
      <li key={node.id} className="space-y-1" data-graph-item={nodeName(node)}>
        <div className="flex flex-wrap items-baseline gap-2">
          {node.level !== 0 && <span className="text-xs text-ink-subtle">{t.graph.level(node.level)}</span>}
          <span className="font-mono text-xs text-ink">{nodeName(node)}</span>
          <span className="text-sm text-ink">{node.requirement?.title ?? titles.get(node.id) ?? ""}</span>
          <span className="text-xs text-ink-muted">
            {nodeMarks(node, reached.get(node.id)?.edge.suspect)
              .map((m) => m.label)
              .join(", ")}
          </span>
          {node.requirement && node.level !== 0 && (
            <WordButton className="text-xs text-accent hover:underline" onClick={() => onCentre(node)}>
              {t.graph.centre}
            </WordButton>
          )}
        </div>
        {(up.length > 0 || down.length > 0) && <ul className="space-y-1 border-l border-border pl-4">{[...up, ...down].map(item)}</ul>}
      </li>
    );
  };
  return (
    <ul aria-label={t.graph.canvas(nodeName(root))} className="space-y-1" data-graph-list>
      {item(root)}
    </ul>
  );
}

/** Everything a change reaches, level by level, nearest first. */
function ImpactList({ graph, titles }: { graph: TraceGraph; titles: Map<string, string> }) {
  const levels: [number, TraceGraphNode[]][] = [];
  for (const node of graph.nodes) {
    if (node.level === 0) continue;
    const last = levels.at(-1);
    if (last && last[0] === node.level) last[1].push(node);
    else levels.push([node.level, [node]]);
  }
  return (
    <Card className="space-y-3 p-4" data-graph-impact>
      <div>
        <h2 className="text-sm font-semibold text-ink">{t.graph.impact}</h2>
        <p className="text-xs text-ink-muted">{t.graph.impactIntro}</p>
      </div>
      {levels.map(([level, nodes]) => (
        <section key={level} aria-label={t.graph.level(level)}>
          <h3 className="text-xs font-semibold text-ink-muted uppercase">{t.graph.level(level)}</h3>
          <ul className="mt-1 space-y-0.5">
            {nodes.map((node) => (
              <li key={node.id} className="text-sm text-ink" data-impact-item={nodeName(node)}>
                <span className="font-mono text-xs">{nodeName(node)}</span> {node.requirement?.title ?? titles.get(node.id) ?? ""}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </Card>
  );
}
