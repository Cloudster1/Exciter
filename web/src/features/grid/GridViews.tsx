import { useState, type FormEvent } from "react";
import type { Project } from "@/api/projects";
import { useViewActions, useViews, type Filter, type Sort, type View, type ViewInput, type ViewKind, type ViewSettings } from "@/api/search";
import { Button, Checkbox, Dialog, ErrorBanner, Field, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { VIEW_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** The grid, matrix or chain report as it stands, which a view saves: a grid's columns, or the others' settings. */
export interface GridState {
  filter: Filter;
  sort: Sort;
  columns?: string[];
  settings?: ViewSettings;
}

/** What a view of a kind keeps of a state. */
function body(kind: ViewKind, state: GridState): ViewInput {
  return kind === "grid"
    ? { filter: state.filter, sort: state.sort, columns: state.columns ?? [] }
    : { filter: state.filter, sort: state.sort, settings: state.settings ?? {} };
}

/** The views of a project's grid, matrix or chain report (#152): one to open, the page saved as a new one, and the
 * open one saved or deleted. */
export function GridViews({
  project,
  kind = "grid",
  viewId,
  state,
  onOpen,
  onNotice,
}: {
  project: Project;
  kind?: ViewKind;
  viewId?: string;
  state: GridState;
  onOpen: (view: View | undefined) => void;
  onNotice: (message: string) => void;
}) {
  const { data: views } = useViews(project.key, kind);
  const actions = useViewActions(project.key);
  const [saving, setSaving] = useState(false);
  const open = views?.find((each) => each.id === viewId);
  const error = actions.update.error ?? actions.remove.error;
  return (
    <div className="flex flex-wrap items-center gap-2" data-grid-views>
      <SelectInput
        aria-label={t.grid.views}
        value={open?.id ?? ""}
        onChange={(e) => onOpen(views?.find((each) => each.id === e.target.value))}
        className="w-auto"
        data-view-select
      >
        <option value="">{kind === "grid" ? t.grid.allRequirements : t.grid.noView}</option>
        {(views ?? []).map((view) => (
          <option key={view.id} value={view.id}>
            {view.shared ? `${view.name} (${t.grid.sharedTag})` : view.name}
          </option>
        ))}
      </SelectInput>
      <Button variant="secondary" size="sm" icon={<Icon.Plus />} onClick={() => setSaving(true)} data-action="save-view">
        {t.grid.saveAs}
      </Button>
      {open?.canEdit && (
        <>
          <Button
            variant="secondary"
            size="sm"
            loading={actions.update.isPending}
            onClick={() => {
              // The banner says why the last action failed, never an earlier one's reason.
              actions.remove.reset();
              actions.update.mutate({ id: open.id, body: body(kind, state) }, { onSuccess: (view) => onNotice(t.grid.viewSaved(view.name)) });
            }}
            data-action="update-view"
          >
            {t.grid.saveChanges}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            icon={<Icon.Trash />}
            loading={actions.remove.isPending}
            onClick={() => {
              actions.update.reset();
              actions.remove.mutate(open.id, {
                onSuccess: () => {
                  onNotice(t.grid.viewDeleted(open.name));
                  onOpen(undefined);
                },
              });
            }}
            data-action="delete-view"
          >
            {t.grid.deleteView}
          </Button>
        </>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {saving && (
        <SaveDialog
          project={project}
          kind={kind}
          state={state}
          onClose={() => setSaving(false)}
          onSaved={(view) => {
            setSaving(false);
            onNotice(t.grid.viewSaved(view.name));
            onOpen(view);
          }}
        />
      )}
    </div>
  );
}

function SaveDialog({
  project,
  kind,
  state,
  onClose,
  onSaved,
}: {
  project: Project;
  kind: ViewKind;
  state: GridState;
  onClose: () => void;
  onSaved: (view: View) => void;
}) {
  const { create } = useViewActions(project.key);
  const [name, setName] = useState("");
  const [shared, setShared] = useState(false);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    create.mutate({ name, shared, kind, ...body(kind, state) }, { onSuccess: onSaved });
  };
  return (
    <Dialog open title={t.grid.saveAs} onClose={onClose} attrs={{ "data-save-view-dialog": "" }}>
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Field
          label={t.grid.viewName}
          value={name}
          maxLength={VIEW_NAME_MAX_LENGTH}
          onChange={(e) => setName(e.target.value)}
          error={create.error?.message}
          required
          data-view-name
        />
        <div className="space-y-1">
          <Checkbox label={t.grid.share} checked={shared} disabled={!project.can.edit} onChange={(e) => setShared(e.target.checked)} data-view-shared />
          {!project.can.edit && <p className="text-xs text-ink-subtle">{t.grid.shareHint}</p>}
        </div>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.grid.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} data-action="confirm-save-view">
            {t.grid.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
