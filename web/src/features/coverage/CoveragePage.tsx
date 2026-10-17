import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  DIMENSIONS,
  LEVELS,
  useCoverage,
  useCoverageRuleActions,
  useCoverageRules,
  type CoverageRule,
  type CoverageTotals,
  type Dimension,
  type Level,
  type LevelFilter,
} from "@/api/coverage";
import { useTypes } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, IconButton, PageHeader, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { COVERAGE_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { Pages } from "./Pages";

/** A project's coverage: the report and its rules, and the way to its traceability matrix. */
export function CoveragePage({ project }: { project: Project }) {
  return (
    <div className="mx-auto max-w-5xl space-y-4" data-coverage={project.key}>
      <PageHeader
        title={t.coverage.title}
        meta={t.coverage.intro}
        actions={
          <Link
            to="/p/$projectKey/matrix"
            params={{ projectKey: project.key }}
            className="inline-flex items-center gap-1.5 text-sm font-medium text-accent hover:underline"
            data-action="open-matrix"
          >
            <Icon.Table className="size-4" />
            {t.matrix.open}
          </Link>
        }
      />
      <Report project={project} />
      <Rules project={project} />
    </div>
  );
}

const levelTone: Record<Level, string> = {
  full: "bg-success-subtle text-success",
  partial: "bg-warning-subtle text-warning",
  none: "bg-danger-subtle text-danger",
  na: "",
};

function share(n: number, total: number): number {
  return total === 0 ? 0 : Math.round((n / total) * 100);
}

/** One dimension's totals as a bar of full, partial and none. */
function Bar({ dimension, totals }: { dimension: Dimension; totals: CoverageTotals }) {
  return (
    <div className="space-y-1" data-dimension={dimension}>
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="font-medium text-ink">{t.coverage.dimensions[dimension]}</span>
        <span className="text-ink-muted tabular-nums" data-full-share="">
          {totals.total === 0 ? t.coverage.notAsked : t.coverage.percent(share(totals.full, totals.total))}
        </span>
      </div>
      <div className="flex h-2 overflow-hidden rounded-full bg-surface-sunken" aria-hidden="true">
        <span className="bg-success" style={{ width: `${share(totals.full, totals.total)}%` }} />
        <span className="bg-warning" style={{ width: `${share(totals.partial, totals.total)}%` }} />
        <span className="bg-danger" style={{ width: `${share(totals.none, totals.total)}%` }} />
      </div>
      {totals.total > 0 && <p className="text-xs text-ink-subtle">{t.coverage.totals(totals.full, totals.partial, totals.none)}</p>}
    </div>
  );
}

function Report({ project }: { project: Project }) {
  const [only, setOnly] = useState<LevelFilter>({});
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useCoverage(project.key, only, offset);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  const keep = (dimension: Dimension, level: string) => {
    setOffset(0);
    setOnly({ ...only, [dimension]: level || undefined });
  };
  return (
    <>
      <Card className="grid gap-4 p-4 sm:grid-cols-3">
        {DIMENSIONS.map((d) => (
          <Bar key={d} dimension={d} totals={data.summary[d]} />
        ))}
      </Card>
      <Card className="space-y-3 p-4">
        <div className="grid gap-3 sm:grid-cols-3">
          {DIMENSIONS.map((d) => (
            <Select key={d} label={t.coverage.dimensions[d]} value={only[d] ?? ""} onChange={(event) => keep(d, event.target.value)} controlSize="sm">
              <option value="">{t.coverage.anyLevel}</option>
              {LEVELS.map((level) => (
                <option key={level} value={level}>
                  {t.coverage.levels[level]}
                </option>
              ))}
            </Select>
          ))}
        </div>
        {data.rows.length === 0 ? (
          <EmptyState icon={<Icon.Check />} title={t.coverage.noRows} />
        ) : (
          <ul className="flex flex-col divide-y divide-border" aria-label={t.coverage.requirements}>
            {data.rows.map((row) => (
              <li key={row.requirement.id} className="flex flex-wrap items-center gap-2 py-2" data-coverage-row={row.requirement.key}>
                <Link
                  to="/p/$projectKey/r/$reqKey"
                  params={{ projectKey: project.key, reqKey: row.requirement.key }}
                  className="min-w-0 flex-1 truncate text-sm text-ink hover:text-accent hover:underline"
                >
                  <span className="font-mono text-xs text-ink-muted">{row.requirement.key}</span> {row.requirement.title}
                </Link>
                {DIMENSIONS.map((d) => (
                  <Tag key={d} className={levelTone[row[d]]} data-level={`${d}:${row[d]}`} title={t.coverage.dimensions[d]}>
                    {`${t.coverage.dimensions[d]}: ${t.coverage.levels[row[d]]}`}
                  </Tag>
                ))}
              </li>
            ))}
          </ul>
        )}
        <Pages offset={offset} shown={data.rows.length} total={data.total} size={COVERAGE_PAGE_SIZE} label={t.coverage.requirements} onOffset={setOffset} />
      </Card>
    </>
  );
}

/** What coverage asks of each type; its administrators tick and untick it in place. */
/** What verified may count, as the rule's select offers it: links, test results, or either (#200). */
const VERIFIED_SOURCES = { links: ["links"], tests: ["tests"], both: ["links", "tests"] } satisfies Record<string, CoverageRule["verifiedFrom"]>;
type SourcesChoice = keyof typeof VERIFIED_SOURCES;

function sourcesOf(from: CoverageRule["verifiedFrom"]): SourcesChoice {
  const links = from.includes("links");
  const tests = from.includes("tests");
  return links && tests ? "both" : tests ? "tests" : "links";
}

function Rules({ project }: { project: Project }) {
  const { data: rules, error, refetch } = useCoverageRules(project.key);
  const { data: types } = useTypes(project.key);
  const { save, remove } = useCoverageRuleActions(project.key);
  const [adding, setAdding] = useState("");
  const mayKeep = project.can.administer && !project.archivedAt;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!rules) return <Skeleton />;
  const everyRule = rules.find((r) => r.typeId === null);
  const open = (types ?? []).filter((type) => !type.retired && !rules.some((r) => r.typeId === type.id));
  const set = (rule: Pick<CoverageRule, "typeId" | "verifiedFrom" | Dimension>) =>
    save.mutate({
      typeId: rule.typeId ?? undefined,
      decomposed: rule.decomposed,
      implemented: rule.implemented,
      verified: rule.verified,
      verifiedFrom: rule.verifiedFrom,
    });
  const rows: Array<Pick<CoverageRule, "typeId" | "typeName" | "verifiedFrom" | Dimension> & { id?: string }> = everyRule
    ? rules
    : [{ typeId: null, typeName: "", decomposed: false, implemented: false, verified: false, verifiedFrom: ["links"] }, ...rules];
  return (
    <Card className="space-y-3 p-4" data-coverage-rules="">
      <SectionTitle>{t.coverage.rulesTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.coverage.rulesIntro}</p>
      {rules.length === 0 && <p className="text-sm text-ink-muted">{t.coverage.noRules}</p>}
      {!mayKeep && <p className="text-sm text-ink-muted">{t.coverage.rulesReadOnly}</p>}
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <ul className="flex flex-col divide-y divide-border" aria-label={t.coverage.rulesTitle}>
        {rows.map((rule) => {
          const name = rule.typeId === null ? t.coverage.everyType : rule.typeName;
          return (
            <li key={rule.typeId ?? "every"} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2" data-rule={name}>
              <span className="min-w-40 flex-1 text-sm font-medium text-ink">{name}</span>
              {DIMENSIONS.map((d) => (
                <Checkbox
                  key={d}
                  label={t.coverage.dimensions[d]}
                  checked={rule[d]}
                  disabled={!mayKeep || save.isPending}
                  onChange={(event) => set({ ...rule, [d]: event.target.checked })}
                />
              ))}
              {rule.verified && (
                <div className="min-w-40">
                  <Select
                    label={t.evidence.sources.legend}
                    aria-label={`${t.evidence.sources.legend}: ${name}`}
                    title={t.evidence.sources.hint}
                    value={sourcesOf(rule.verifiedFrom)}
                    disabled={!mayKeep || save.isPending}
                    controlSize="sm"
                    onChange={(event) => set({ ...rule, verifiedFrom: VERIFIED_SOURCES[event.target.value as SourcesChoice] })}
                    data-verified-from={name}
                  >
                    {(Object.keys(VERIFIED_SOURCES) as SourcesChoice[]).map((choice) => (
                      <option key={choice} value={choice}>
                        {VERIFIED_SOURCES[choice].map((source) => t.evidence.sources[source]).join(" + ")}
                      </option>
                    ))}
                  </Select>
                </div>
              )}
              {mayKeep && rule.id && rule.typeId !== null && (
                <IconButton
                  label={t.coverage.removeRule(name)}
                  icon={<Icon.Trash />}
                  size="sm"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(rule.id!)}
                />
              )}
            </li>
          );
        })}
      </ul>
      {mayKeep && open.length > 0 && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (!adding) return;
            save.mutate({ typeId: adding, decomposed: false, implemented: false, verified: false }, { onSuccess: () => setAdding("") });
          }}
        >
          <div className="min-w-48">
            <Select label={t.coverage.addRule} value={adding} onChange={(event) => setAdding(event.target.value)} controlSize="sm">
              <option value="" />
              {open.map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
            </Select>
          </div>
          <Button type="submit" variant="secondary" size="sm" disabled={!adding}>
            {t.coverage.addRuleSend}
          </Button>
        </form>
      )}
    </Card>
  );
}
