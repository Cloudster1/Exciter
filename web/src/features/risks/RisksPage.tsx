import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { riskReportHref, risksHref, useRiskRows, useRiskScheme } from "@/api/risks";
import { Button, ButtonLink, EmptyState, ErrorBanner, PageHeader, Skeleton, TabPanel, Tabs } from "@/components/ui";
import { Icon } from "@/components/icons";
import { QueryInput, queryFailure } from "@/features/query/QueryInput";
import { RISK_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { FmeaTable } from "./FmeaTable";
import { RiskMatrixView } from "./RiskMatrixView";

/** The views of a project's risks, as the address names them. */
export type RisksView = "table" | "matrix";

/** A project's risks (#260): the FMEA table, edited in place and exported as Excel, and the risk matrix. */
export function RisksPage({ project, view }: { project: Project; view: RisksView }) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const params = { query: query || undefined };
  return (
    <div className="space-y-4" data-risks-page={view}>
      <PageHeader
        title={t.risks.nav}
        meta={project.name}
        actions={
          <span className="flex flex-wrap gap-2">
            {view === "table" && (
              <ButtonLink href={risksHref(project.key, params)} icon={<Icon.Download />} data-action="export-risks">
                {t.risks.exportXlsx}
              </ButtonLink>
            )}
            <ButtonLink href={riskReportHref(project.key, "pdf", query)} icon={<Icon.Download />} data-action="risk-report-pdf">
              {t.risks.reportPdf}
            </ButtonLink>
            <ButtonLink href={riskReportHref(project.key, "docx", query)} icon={<Icon.Download />} data-action="risk-report-docx">
              {t.risks.reportWord}
            </ButtonLink>
          </span>
        }
        tabs={
          <Tabs<RisksView>
            label={t.risks.nav}
            value={view}
            onChange={(next) => void navigate({ to: "/projects/$projectKey/risks", params: { projectKey: project.key }, search: { view: next } })}
            panelId="risks-panel"
            tabs={[
              { value: "table", label: t.risks.views.table, attrs: { "data-tab": "table" } },
              { value: "matrix", label: t.risks.views.matrix, attrs: { "data-tab": "matrix" } },
            ]}
          />
        }
      />
      <TabPanel id="risks-panel" className="space-y-4">
        {view === "table" ? (
          <TableView project={project} query={query} onQuery={setQuery} />
        ) : (
          <RiskMatrixView project={project} query={query} onQuery={setQuery} />
        )}
      </TabPanel>
    </div>
  );
}

function TableView({ project, query, onQuery }: { project: Project; query: string; onQuery: (query: string) => void }) {
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useRiskRows(project.key, { query: query || undefined }, offset, RISK_PAGE_SIZE);
  const { data: scheme } = useRiskScheme(project.key);
  const failed = queryFailure(error);
  return (
    <>
      <QueryInput
        projectKey={project.key}
        value={query}
        error={error}
        onSubmit={(next) => {
          onQuery(next);
          setOffset(0);
        }}
      />
      {error && !failed && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && data.unrated > 0 && (
        <p role="status" className="text-sm text-ink-muted" data-unrated={data.unrated}>
          {t.risks.unrated(data.unrated)}
        </p>
      )}
      {data && data.total === 0 && <EmptyState title={t.risks.noRisks} />}
      {data && data.risks.length > 0 && <FmeaTable project={project} rows={data.risks} scheme={scheme ?? null} />}
      {data && data.total > RISK_PAGE_SIZE && (
        <div className="flex items-center gap-2 text-sm text-ink-muted">
          <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - RISK_PAGE_SIZE))}>
            {t.risks.previous}
          </Button>
          <span>{t.risks.shown(offset + 1, Math.min(offset + RISK_PAGE_SIZE, data.total), data.total)}</span>
          <Button size="sm" variant="ghost" disabled={offset + RISK_PAGE_SIZE >= data.total} onClick={() => setOffset(offset + RISK_PAGE_SIZE)}>
            {t.risks.next}
          </Button>
        </div>
      )}
    </>
  );
}
