import { useState, type DragEvent, type ReactNode } from "react";
import { useDashboardActions, useWidgetData, type Dashboard, type Widget } from "@/api/dashboards";
import { Icon } from "@/components/icons";
import { Button, Card, cx, EmptyState, ErrorBanner, IconButton, Select, Skeleton } from "@/components/ui";
import { DASHBOARD_COLUMNS } from "@/config";
import { t } from "@/i18n";
import { WidgetBody } from "./WidgetBody";
import { WidgetForm } from "./WidgetForm";

const WIDTHS = Array.from({ length: DASHBOARD_COLUMNS }, (_, i) => i + 1);
/** The column spans by width, literal so Tailwind sees them. */
export const SPAN: Record<number, string> = { 1: "md:col-span-1", 2: "md:col-span-2", 3: "md:col-span-3" };

/** A dashboard's widgets in order and width; arranging, they are dragged into place or moved by their buttons. */
export function DashboardGrid({ projectKey, dashboard, arranging }: { projectKey: string; dashboard: Dashboard; arranging: boolean }) {
  const actions = useDashboardActions(projectKey);
  const [dragging, setDragging] = useState<string>();
  const [editing, setEditing] = useState<string>();
  const [adding, setAdding] = useState(false);
  const widgets = dashboard.widgets;
  const failed = actions.order.error ?? actions.removeWidget.error;

  const moveTo = (id: string, to: number) => {
    const order = widgets.map((w) => w.id).filter((each) => each !== id);
    order.splice(Math.max(0, Math.min(to, order.length)), 0, id);
    actions.order.mutate({ dashboardId: dashboard.id, widgets: order });
  };
  const drop = (event: DragEvent, at: number) => {
    event.preventDefault();
    if (dragging) moveTo(dragging, at);
    setDragging(undefined);
  };

  return (
    <div className="space-y-4">
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      {arranging && <p className="text-sm text-ink-muted">{t.dashboard.dragHint}</p>}
      {widgets.length === 0 && !adding ? (
        <EmptyState title={t.dashboard.nothingHere} />
      ) : (
        <ol className="grid gap-4 md:grid-cols-3" data-dashboard-grid>
          {widgets.map((widget, index) => (
            <li
              key={widget.id}
              className={cx(SPAN[widget.width], dragging === widget.id && "opacity-50")}
              draggable={arranging && editing !== widget.id}
              onDragStart={() => setDragging(widget.id)}
              onDragEnd={() => setDragging(undefined)}
              onDragOver={(event) => arranging && event.preventDefault()}
              onDrop={(event) => drop(event, index)}
              data-widget={widget.title}
            >
              {editing === widget.id ? (
                <Card className="p-4">
                  <WidgetForm
                    projectKey={projectKey}
                    widget={widget}
                    pending={actions.updateWidget.isPending}
                    error={actions.updateWidget.error}
                    onCancel={() => setEditing(undefined)}
                    onSave={(input) =>
                      actions.updateWidget.mutate(
                        { id: widget.id, dashboardId: dashboard.id, title: input.title, width: input.width, config: input.config },
                        { onSuccess: () => setEditing(undefined) },
                      )
                    }
                  />
                </Card>
              ) : (
                <WidgetCard
                  projectKey={projectKey}
                  widget={widget}
                  controls={
                    arranging && (
                      <div className="flex shrink-0 items-center gap-0.5" data-widget-controls>
                        <IconButton
                          size="sm"
                          icon={<Icon.ChevronUp />}
                          label={t.dashboard.moveEarlier(widget.title)}
                          disabled={index === 0}
                          onClick={() => moveTo(widget.id, index - 1)}
                        />
                        <IconButton
                          size="sm"
                          icon={<Icon.ChevronDown />}
                          label={t.dashboard.moveLater(widget.title)}
                          disabled={index === widgets.length - 1}
                          onClick={() => moveTo(widget.id, index + 1)}
                        />
                        <div className="w-28">
                          <Select
                            label={t.dashboard.width}
                            id={`width-${widget.id}`}
                            controlSize="sm"
                            value={widget.width}
                            onChange={(event) => actions.updateWidget.mutate({ id: widget.id, dashboardId: dashboard.id, width: Number(event.target.value) })}
                          >
                            {WIDTHS.map((w) => (
                              <option key={w} value={w}>
                                {t.dashboard.widths(w)}
                              </option>
                            ))}
                          </Select>
                        </div>
                        <IconButton size="sm" icon={<Icon.Edit />} label={t.dashboard.editWidget(widget.title)} onClick={() => setEditing(widget.id)} />
                        <IconButton
                          size="sm"
                          icon={<Icon.Trash />}
                          label={t.dashboard.removeWidget(widget.title)}
                          onClick={() => actions.removeWidget.mutate({ id: widget.id, dashboardId: dashboard.id })}
                        />
                      </div>
                    )
                  }
                />
              )}
            </li>
          ))}
        </ol>
      )}
      {arranging && adding && (
        <Card className="p-4">
          <WidgetForm
            projectKey={projectKey}
            pending={actions.addWidget.isPending}
            error={actions.addWidget.error}
            onCancel={() => setAdding(false)}
            onSave={(input) => actions.addWidget.mutate({ dashboardId: dashboard.id, ...input }, { onSuccess: () => setAdding(false) })}
          />
        </Card>
      )}
      {arranging && !adding && <AddButton onClick={() => setAdding(true)} />}
    </div>
  );
}

function AddButton({ onClick }: { onClick: () => void }) {
  return (
    <Button icon={<Icon.Plus />} variant="secondary" onClick={onClick} data-action="add-widget">
      {t.dashboard.addWidget}
    </Button>
  );
}

/** One widget: its title, its controls while arranging, and what it shows, read as the reader. */
export function WidgetCard({ projectKey, widget, controls }: { projectKey: string; widget: Widget; controls?: ReactNode }) {
  const { data, error } = useWidgetData(widget.id);
  return (
    <Card className="h-full p-4" data-widget-card={widget.kind}>
      <div className="mb-3 flex items-start justify-between gap-2">
        <h2 className="text-sm font-semibold text-ink">{widget.title}</h2>
        {controls}
      </div>
      {error ? (
        <p className="text-sm text-danger">
          {t.dashboard.failed} {error.message}
        </p>
      ) : data ? (
        <WidgetBody projectKey={projectKey} kind={widget.kind} config={widget.config} data={data} />
      ) : (
        <Skeleton />
      )}
    </Card>
  );
}
