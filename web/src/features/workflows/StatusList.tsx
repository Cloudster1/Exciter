import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { STATUS_CATEGORIES, useStatusActions, useStatuses, type Status, type StatusCategory } from "@/api/workflows";
import { Button, Card, Checkbox, ErrorBanner, Field, IconButton, Select, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { STATUS_DESCRIPTION_MAX_LENGTH, STATUS_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { NewStatusForm } from "./designer/NewStatusForm";
import { StatusBadge } from "./StatusBadge";

/** The table's columns, which a status opened for editing spans. */
const STATUS_COLUMNS = 3;

/** A project's statuses in order: its administrators add, rename, move and delete those nothing uses. */
export function StatusList({ project, mayChange }: { project: Project; mayChange: boolean }) {
  const { data: statuses, error, refetch } = useStatuses(project.key);
  const actions = useStatusActions(project.key);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const failed = actions.update.error ?? actions.remove.error;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!statuses) return <Skeleton />;
  return (
    <div className="space-y-3" data-status-list>
      <p className="text-sm text-ink-muted">{t.workflows.statusesIntro}</p>
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      <Table aria-label={t.workflows.statusesTitle}>
        <thead>
          <tr>
            <Th>{t.workflows.statusName}</Th>
            <Th className="w-32">{t.workflows.columnRequirements}</Th>
            {mayChange && (
              <Th>
                <span className="sr-only">{t.workflows.columnActions}</span>
              </Th>
            )}
          </tr>
        </thead>
        <tbody>
          {statuses.map((status, index) =>
            editing === status.id ? (
              <tr key={status.id} data-status-row={status.name}>
                <Td colSpan={STATUS_COLUMNS}>
                  <StatusForm projectKey={project.key} status={status} onDone={() => setEditing(null)} />
                </Td>
              </tr>
            ) : (
              <tr key={status.id} data-status-row={status.name}>
                <Td>
                  <StatusBadge name={status.name} category={status.category} />
                  {status.locks && (
                    <Tag className="ml-2" data-status-locks="">
                      <Icon.Lock className="mr-1 size-3" aria-hidden="true" />
                      {t.control.statusLocked}
                    </Tag>
                  )}
                  {status.description && <span className="mt-1 block text-xs text-ink-subtle">{status.description}</span>}
                </Td>
                <Td>
                  <span className="text-ink-muted">{status.requirements}</span>
                </Td>
                {mayChange && (
                  <Td className="text-right whitespace-nowrap">
                    <IconButton
                      icon={<Icon.ChevronDown className="rotate-180" />}
                      label={t.workflows.moveUp(status.name)}
                      size="sm"
                      disabled={index === 0}
                      onClick={() => actions.update.mutate({ id: status.id, position: index - 1 })}
                    />
                    <IconButton
                      icon={<Icon.ChevronDown />}
                      label={t.workflows.moveDown(status.name)}
                      size="sm"
                      disabled={index === statuses.length - 1}
                      onClick={() => actions.update.mutate({ id: status.id, position: index + 1 })}
                    />
                    <Button variant="ghost" size="sm" onClick={() => setEditing(status.id)} data-action="edit-status">
                      {t.workflows.edit}
                    </Button>
                    {status.requirements === 0 && (
                      <Button variant="ghost" size="sm" onClick={() => actions.remove.mutate(status.id)} data-action="delete-status">
                        {t.workflows.delete}
                      </Button>
                    )}
                  </Td>
                )}
              </tr>
            ),
          )}
        </tbody>
      </Table>
      {mayChange &&
        (adding ? (
          <Card className="p-4">
            <NewStatusForm
              projectKey={project.key}
              heading={t.workflows.newStatusHeading}
              submitLabel={t.workflows.createStatus}
              onCreated={() => setAdding(false)}
            />
            <Button size="sm" variant="ghost" className="mt-2" onClick={() => setAdding(false)}>
              {t.kit.cancel}
            </Button>
          </Card>
        ) : (
          <Button icon={<Icon.Plus />} variant="secondary" onClick={() => setAdding(true)} data-action="new-status">
            {t.workflows.newStatus}
          </Button>
        ))}
    </div>
  );
}

/** Changes a status's name, category, description or whether it locks its requirements, in place. */
function StatusForm({ projectKey, status, onDone }: { projectKey: string; status: Status; onDone: () => void }) {
  const { update } = useStatusActions(projectKey);
  const [name, setName] = useState(status.name);
  const [category, setCategory] = useState<StatusCategory>(status.category);
  const [description, setDescription] = useState(status.description);
  const [locks, setLocks] = useState(status.locks);
  const fields = update.error instanceof ApiError ? update.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate({ id: status.id, name: name.trim(), category, description: description.trim(), locks }, { onSuccess: onDone });
  }

  return (
    <form onSubmit={submit} className="space-y-3" noValidate data-status-form>
      {update.error && Object.keys(fields).length === 0 && <ErrorBanner>{update.error.message}</ErrorBanner>}
      <div className="grid gap-3 sm:grid-cols-[1fr_12rem]">
        <Field
          label={t.workflows.statusName}
          value={name}
          maxLength={STATUS_NAME_MAX_LENGTH}
          onChange={(event) => setName(event.target.value)}
          error={fields.name}
        />
        <Select label={t.workflows.countsAs} value={category} onChange={(event) => setCategory(event.target.value as StatusCategory)} error={fields.category}>
          {STATUS_CATEGORIES.map((each) => (
            <option key={each} value={each}>
              {t.workflows.categories[each]}
            </option>
          ))}
        </Select>
      </div>
      <Field
        label={t.workflows.description}
        value={description}
        maxLength={STATUS_DESCRIPTION_MAX_LENGTH}
        onChange={(event) => setDescription(event.target.value)}
        error={fields.description}
      />
      <div>
        <Checkbox
          label={t.control.statusLocks}
          checked={locks}
          onChange={(event) => setLocks(event.target.checked)}
          aria-describedby={`status-locks-hint-${status.id}`}
          data-field="status-locks"
        />
        <p id={`status-locks-hint-${status.id}`} className="ml-6 text-xs text-ink-muted">
          {t.control.statusLocksHint}
        </p>
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" loading={update.isPending} data-action="save-status">
          {t.workflows.saveStatus}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onDone}>
          {t.kit.cancel}
        </Button>
      </div>
    </form>
  );
}
