import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useTypeActions, useTypes, type RequirementType } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { Button, Card, ErrorBanner, Field, IconButton, Select, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TYPE_COLOURS, TYPE_ICONS } from "@/config";
import { t } from "@/i18n";
import { TypeIcon } from "./TypeIcon";

/** The table's columns, which a type opened for editing spans. */
const TYPE_COLUMNS = 3;

/** A project's requirement types in order: its administrators add, rename, move, retire and delete them. */
export function TypeList({ project }: { project: Project }) {
  const { data: types, error, refetch } = useTypes(project.key);
  const actions = useTypeActions(project.key);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const mayChange = project.can.administer && !project.archivedAt;
  const failed = actions.update.error ?? actions.remove.error;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!types) return <Skeleton />;
  return (
    <div className="space-y-4" data-type-list>
      <p className="text-sm text-ink-muted">{t.types.intro}</p>
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      <Table aria-label={t.types.tableLabel}>
        <thead>
          <tr>
            <Th>{t.types.columnName}</Th>
            <Th className="w-32">{t.types.columnRequirements}</Th>
            {mayChange && (
              <Th>
                <span className="sr-only">{t.types.columnActions}</span>
              </Th>
            )}
          </tr>
        </thead>
        <tbody>
          {types.map((type, index) =>
            editing === type.id ? (
              <tr key={type.id} data-type={type.name}>
                <Td colSpan={TYPE_COLUMNS}>
                  <TypeForm projectKey={project.key} type={type} onDone={() => setEditing(null)} />
                </Td>
              </tr>
            ) : (
              <tr key={type.id} data-type={type.name} data-retired={type.retired ? "" : undefined}>
                <Td>
                  <span className="flex items-center gap-2 font-medium text-ink">
                    <TypeIcon icon={type.icon} colour={type.colour} />
                    {type.name}
                    {type.id === project.settings.defaultTypeId && <Tag>{t.types.default}</Tag>}
                    {type.retired && <Tag>{t.types.retired}</Tag>}
                  </span>
                  {type.description && <span className="block text-xs text-ink-subtle">{type.description}</span>}
                </Td>
                <Td>
                  <span className="text-ink-muted">{type.requirements}</span>
                </Td>
                {mayChange && (
                  <Td className="text-right whitespace-nowrap">
                    <IconButton
                      icon={<Icon.ChevronDown className="rotate-180" />}
                      label={t.types.moveUp(type.name)}
                      size="sm"
                      disabled={index === 0}
                      onClick={() => actions.update.mutate({ id: type.id, position: index - 1 })}
                    />
                    <IconButton
                      icon={<Icon.ChevronDown />}
                      label={t.types.moveDown(type.name)}
                      size="sm"
                      disabled={index === types.length - 1}
                      onClick={() => actions.update.mutate({ id: type.id, position: index + 1 })}
                    />
                    <Button variant="ghost" size="sm" onClick={() => setEditing(type.id)} data-action="edit-type">
                      {t.types.edit}
                    </Button>
                    {type.id !== project.settings.defaultTypeId && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => actions.update.mutate({ id: type.id, retired: !type.retired })}
                        data-action={type.retired ? "restore-type" : "retire-type"}
                      >
                        {type.retired ? t.types.unretire : t.types.retire}
                      </Button>
                    )}
                    {type.requirements === 0 && type.id !== project.settings.defaultTypeId && (
                      <Button variant="ghost" size="sm" onClick={() => actions.remove.mutate(type.id)} data-action="delete-type">
                        {t.types.delete}
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
            <TypeForm projectKey={project.key} onDone={() => setAdding(false)} />
          </Card>
        ) : (
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-type">
            {t.types.create}
          </Button>
        ))}
    </div>
  );
}

/** Makes a type, or changes the one given. */
function TypeForm({ projectKey, type, onDone }: { projectKey: string; type?: RequirementType; onDone: () => void }) {
  const actions = useTypeActions(projectKey);
  const save = type ? actions.update : actions.create;
  const [name, setName] = useState(type?.name ?? "");
  const [description, setDescription] = useState(type?.description ?? "");
  const [icon, setIcon] = useState(type?.icon ?? TYPE_ICONS[0]);
  const [colour, setColour] = useState(type?.colour ?? TYPE_COLOURS[0]);
  const fields = save.error instanceof ApiError ? save.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    const body = { name, description, icon, colour };
    if (type) actions.update.mutate({ id: type.id, ...body }, { onSuccess: onDone });
    else actions.create.mutate(body, { onSuccess: onDone });
  }

  return (
    <form onSubmit={submit} className="space-y-3" noValidate data-type-form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <div className="grid gap-3 sm:grid-cols-[1fr_10rem_10rem]">
        <Field label={t.types.name} value={name} onChange={(event) => setName(event.target.value)} error={fields.name} autoFocus required />
        <Select label={t.types.icon} value={icon} onChange={(event) => setIcon(event.target.value)} error={fields.icon}>
          {TYPE_ICONS.map((each) => (
            <option key={each} value={each}>
              {t.types.icons[each]}
            </option>
          ))}
        </Select>
        <Select label={t.types.colour} value={colour} onChange={(event) => setColour(event.target.value)} error={fields.colour}>
          {TYPE_COLOURS.map((each, i) => (
            <option key={each} value={each}>
              {t.types.colours[i]}
            </option>
          ))}
        </Select>
      </div>
      <Field label={t.types.description} value={description} onChange={(event) => setDescription(event.target.value)} error={fields.description} />
      <div className="flex items-center gap-2">
        <TypeIcon icon={icon} colour={colour} />
        <Button type="submit" size="sm" loading={save.isPending} data-action="save-type">
          {type ? t.types.save : t.types.create}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onDone}>
          {t.types.cancel}
        </Button>
      </div>
    </form>
  );
}
