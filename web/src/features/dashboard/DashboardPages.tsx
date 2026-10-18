import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { dashboardPdfHref, useDashboard, useDashboardActions, useDashboards, useDashboardTemplates, useWidgetData, type Widget } from "@/api/dashboards";
import type { Project } from "@/api/projects";
import { Icon } from "@/components/icons";
import { Button, ButtonLink, Card, Checkbox, EmptyState, ErrorBanner, Field, PageHeader, Select, Skeleton, Tag } from "@/components/ui";
import { usePrintReady } from "@/features/print/printReady";
import { useLightScheme } from "@/features/print/PrintDocument";
import { t } from "@/i18n";
import { DashboardGrid, SPAN, WidgetCard } from "./DashboardGrid";
import { WidgetBody } from "./WidgetBody";

/** A project's dashboards (#212): the reader's own and the shared ones, and a new one from a template. */
export function DashboardsPage({ project }: { project: Project }) {
  const { data: dashboards, error, refetch } = useDashboards(project.key);
  const [making, setMaking] = useState(false);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!dashboards) return <Skeleton />;
  const mine = dashboards.filter((d) => d.can.edit && !d.shared);
  const shared = dashboards.filter((d) => d.shared);
  return (
    <div className="space-y-5" data-dashboards={project.key}>
      <PageHeader
        title={t.dashboard.title}
        meta={t.dashboard.intro}
        actions={
          !making && (
            <Button icon={<Icon.Plus />} onClick={() => setMaking(true)} data-action="new-dashboard">
              {t.dashboard.newTitle}
            </Button>
          )
        }
      />
      {making && <NewDashboard project={project} onCancel={() => setMaking(false)} />}
      {dashboards.length === 0 && !making && <EmptyState title={t.dashboard.none} />}
      {[
        { title: t.dashboard.mine, list: mine },
        { title: t.dashboard.shared, list: shared },
      ].map(
        ({ title, list }) =>
          list.length > 0 && (
            <section key={title} aria-label={title} className="space-y-2">
              <h2 className="text-sm font-semibold text-ink">{title}</h2>
              <ul className="grid gap-2 sm:grid-cols-2">
                {list.map((d) => (
                  <li key={d.id}>
                    <Card className="flex items-center justify-between gap-2 p-3" data-dashboard-item={d.name}>
                      <Link
                        to="/p/$projectKey/dashboards/$dashboardId"
                        params={{ projectKey: project.key, dashboardId: d.id }}
                        className="font-medium text-ink hover:text-accent hover:underline"
                      >
                        {d.name}
                      </Link>
                      <span className="flex items-center gap-1.5 text-xs text-ink-muted">
                        {d.home && <Tag>{t.dashboard.homeTag}</Tag>}
                        {t.dashboard.owner(d.owner.name)}
                      </span>
                    </Card>
                  </li>
                ))}
              </ul>
            </section>
          ),
      )}
    </div>
  );
}

function NewDashboard({ project, onCancel }: { project: Project; onCancel: () => void }) {
  const actions = useDashboardActions(project.key);
  const { data: catalogue } = useDashboardTemplates();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [template, setTemplate] = useState("overview");
  const [shared, setShared] = useState(false);
  const fields = actions.create.error instanceof ApiError ? actions.create.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    actions.create.mutate(
      { name, shared, template: template || undefined },
      { onSuccess: (d) => void navigate({ to: "/p/$projectKey/dashboards/$dashboardId", params: { projectKey: project.key, dashboardId: d.id } }) },
    );
  }
  return (
    <Card className="p-4">
      <form onSubmit={submit} className="space-y-3" noValidate data-new-dashboard>
        {actions.create.error && Object.keys(fields).length === 0 && <ErrorBanner>{actions.create.error.message}</ErrorBanner>}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t.dashboard.name} value={name} onChange={(event) => setName(event.target.value)} error={fields.name} autoFocus required />
          <Select label={t.dashboard.template} value={template} onChange={(event) => setTemplate(event.target.value)} error={fields.template}>
            <option value="">{t.dashboard.empty}</option>
            {(catalogue?.templates ?? []).map((each) => (
              <option key={each.key} value={each.key}>
                {each.name}
              </option>
            ))}
          </Select>
        </div>
        {project.can.edit && <Checkbox label={t.dashboard.share} checked={shared} onChange={(event) => setShared(event.target.checked)} />}
        <div className="flex gap-2">
          <Button type="submit" size="sm" loading={actions.create.isPending} data-action="create-dashboard">
            {t.dashboard.create}
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={onCancel}>
            {t.dashboard.cancel}
          </Button>
        </div>
      </form>
    </Card>
  );
}

/** One dashboard, read or arranged; its link is its address, which whoever reads the project opens once shared. */
export function DashboardPage({ project, dashboardId }: { project: Project; dashboardId: string }) {
  const { data: dashboard, error, refetch } = useDashboard(dashboardId);
  const actions = useDashboardActions(project.key);
  const navigate = useNavigate();
  const [arranging, setArranging] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState("");
  const [notice, setNotice] = useState("");
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!dashboard) return <Skeleton />;
  const failed = actions.update.error ?? actions.remove.error;
  const copy = async () => {
    await navigator.clipboard.writeText(window.location.href);
    setNotice(t.dashboard.copied);
  };
  return (
    <div className="space-y-4" data-dashboard={dashboard.name}>
      <PageHeader
        crumb={
          <Link to="/p/$projectKey/dashboards" params={{ projectKey: project.key }} className="hover:text-accent hover:underline">
            {t.dashboard.back}
          </Link>
        }
        title={dashboard.name}
        meta={
          <span className="flex flex-wrap items-center gap-2">
            {dashboard.shared && <Tag>{t.dashboard.sharedTag}</Tag>}
            {dashboard.home && <Tag>{t.dashboard.homeTag}</Tag>}
            {t.dashboard.owner(dashboard.owner.name)}
          </span>
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {dashboard.shared && (
              <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={() => void copy()} data-action="copy-dashboard-link">
                {t.dashboard.copyLink}
              </Button>
            )}
            <ButtonLink href={dashboardPdfHref(dashboard.id)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="dashboard-pdf">
              {t.dashboard.pdf}
            </ButtonLink>
            {dashboard.can.edit && (
              <Button variant={arranging ? "primary" : "secondary"} size="sm" onClick={() => setArranging(!arranging)} data-action="arrange-dashboard">
                {arranging ? t.dashboard.done : t.dashboard.arrange}
              </Button>
            )}
          </div>
        }
      />
      {notice && (
        <p role="status" className="text-sm text-ink-muted">
          {notice}
        </p>
      )}
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      {arranging && (
        <div className="flex flex-wrap items-end gap-2" data-dashboard-settings>
          {renaming ? (
            <form
              className="flex items-end gap-2"
              onSubmit={(event) => {
                event.preventDefault();
                actions.update.mutate({ id: dashboard.id, name }, { onSuccess: () => setRenaming(false) });
              }}
            >
              <Field label={t.dashboard.name} value={name} onChange={(event) => setName(event.target.value)} />
              <Button type="submit" size="sm">
                {t.dashboard.save}
              </Button>
            </form>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setName(dashboard.name);
                setRenaming(true);
              }}
            >
              {t.dashboard.rename}
            </Button>
          )}
          {(dashboard.shared || dashboard.can.share) && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => actions.update.mutate({ id: dashboard.id, shared: !dashboard.shared })}
              data-action="share-dashboard"
            >
              {dashboard.shared ? t.dashboard.unshare : t.dashboard.shareNow}
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              actions.remove.mutate(dashboard.id, { onSuccess: () => void navigate({ to: "/p/$projectKey/dashboards", params: { projectKey: project.key } }) })
            }
            data-action="delete-dashboard"
          >
            {t.dashboard.delete}
          </Button>
        </div>
      )}
      <DashboardGrid projectKey={project.key} dashboard={dashboard} arranging={arranging} />
    </div>
  );
}

/** The project's home dashboard, above its requirements, when its administrators chose one. */
export function HomeDashboard({ project }: { project: Project }) {
  const id = project.settings.homeDashboardId ?? undefined;
  const { data: dashboard } = useDashboard(id);
  if (!id || !dashboard) return null;
  return (
    <section aria-label={dashboard.name} className="mb-6 space-y-2" data-home-dashboard>
      <h2 className="text-sm font-semibold text-ink">
        <Link
          to="/p/$projectKey/dashboards/$dashboardId"
          params={{ projectKey: project.key, dashboardId: dashboard.id }}
          className="hover:text-accent hover:underline"
        >
          {dashboard.name}
        </Link>
      </h2>
      <ol className="grid gap-4 md:grid-cols-3">
        {dashboard.widgets.map((widget) => (
          <li key={widget.id} className={SPAN[widget.width]}>
            <WidgetCard projectKey={project.key} widget={widget} />
          </li>
        ))}
      </ol>
    </section>
  );
}

/** A dashboard as the render service prints it: every widget drawn, then ready. */
export function PrintDashboard({ dashboardId }: { dashboardId: string }) {
  useLightScheme();
  const { data: dashboard, error } = useDashboard(dashboardId);
  const [drawn, setDrawn] = useState<Set<string>>(new Set());
  const ready = Boolean(dashboard) && dashboard!.widgets.every((w) => drawn.has(w.id));
  usePrintReady(ready || Boolean(error));
  if (error) return <p data-print-failed>{error.message}</p>;
  if (!dashboard) return null;
  return (
    <main className="print-view space-y-4" data-print-view>
      <div hidden data-print-meta data-title={dashboard.name} data-header={dashboard.projectKey} data-footer={dashboard.name} />
      <h1 className="text-xl font-semibold text-ink">{dashboard.name}</h1>
      <div className="grid grid-cols-2 gap-4">
        {dashboard.widgets.map((widget) => (
          <PrintWidget
            key={widget.id}
            widget={widget}
            projectKey={dashboard.projectKey}
            onDrawn={() => setDrawn((s) => (s.has(widget.id) ? s : new Set(s).add(widget.id)))}
          />
        ))}
      </div>
    </main>
  );
}

function PrintWidget({ widget, projectKey, onDrawn }: { widget: Widget; projectKey: string; onDrawn: () => void }) {
  const { data, error } = useWidgetData(widget.id);
  const done = Boolean(data || error);
  useEffect(() => {
    if (done) onDrawn();
  }, [done, onDrawn]);
  return (
    <section className={widget.width > 1 ? "col-span-2 break-inside-avoid" : "break-inside-avoid"} aria-label={widget.title}>
      <h2 className="mb-2 text-sm font-semibold text-ink">{widget.title}</h2>
      {data ? <WidgetBody projectKey={projectKey} kind={widget.kind} config={widget.config} data={data} /> : error ? <p>{t.dashboard.failed}</p> : null}
    </section>
  );
}
