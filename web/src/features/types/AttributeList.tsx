import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import {
  useAttributeActions,
  useAttributeKinds,
  useRecompute,
  useAttributes,
  useTypes,
  type Attribute,
  type FieldPermit,
  type Kind,
  type OptionChange,
  type RequirementType,
} from "@/api/attributes";
import type { Project } from "@/api/projects";
import { Button, Card, Checkbox, ErrorBanner, Field, IconButton, Input, Select, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { useStatuses } from "@/api/workflows";
import { PermitsEditor, RequiredFrom } from "./FieldRuleControls";
import { FormulaEditor } from "./FormulaEditor";

/** The table's columns, which an attribute opened for editing spans. */
const ATTRIBUTE_COLUMNS = 4;

const hasOptions = (kind: Kind) => kind === "select" || kind === "multiSelect";

/** A project's attributes in order: its administrators define, change, move, retire and delete them. */
export function AttributeList({ project }: { project: Project }) {
  const { data: attributes, error, refetch } = useAttributes(project.key);
  const { data: types } = useTypes(project.key);
  const { data: kinds } = useAttributeKinds();
  const { data: recompute } = useRecompute(project.key);
  const actions = useAttributeActions(project.key);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const mayChange = project.can.administer && !project.archivedAt;
  const failed = actions.update.error ?? actions.remove.error;
  const kindTitle = (kind: Kind) => kinds?.find((each) => each.kind === kind)?.title ?? kind;
  const typeNames = (ids: string[]) =>
    ids.length === 0 ? t.attributes.everyType : ids.map((id) => types?.find((each) => each.id === id)?.name ?? "").join(", ");
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!attributes) return <Skeleton />;
  return (
    <div className="space-y-4" data-attribute-list>
      <p className="text-sm text-ink-muted">{t.attributes.intro}</p>
      {recompute && (
        <p role="status" className="text-sm text-ink" data-recompute={`${recompute.done}/${recompute.total}`}>
          {t.attributes.recomputing(recompute.done, recompute.total)}
        </p>
      )}
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      {attributes.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.attributes.empty}</p>
      ) : (
        <Table aria-label={t.attributes.tableLabel}>
          <thead>
            <tr>
              <Th>{t.attributes.columnName}</Th>
              <Th className="w-32">{t.attributes.columnKind}</Th>
              <Th>{t.attributes.columnTypes}</Th>
              {mayChange && (
                <Th>
                  <span className="sr-only">{t.attributes.columnActions}</span>
                </Th>
              )}
            </tr>
          </thead>
          <tbody>
            {attributes.map((attribute, index) =>
              editing === attribute.id ? (
                <tr key={attribute.id} data-attribute={attribute.name}>
                  <Td colSpan={ATTRIBUTE_COLUMNS}>
                    <AttributeForm projectKey={project.key} types={types ?? []} attribute={attribute} onDone={() => setEditing(null)} />
                  </Td>
                </tr>
              ) : (
                <tr key={attribute.id} data-attribute={attribute.name} data-retired={attribute.retired ? "" : undefined}>
                  <Td>
                    <span className="flex flex-wrap items-center gap-2 font-medium text-ink">
                      {attribute.name}
                      {attribute.required && <Tag>{t.attributes.required}</Tag>}
                      {attribute.formula !== null && <Tag data-computed-tag="">{t.attributes.computed}</Tag>}
                      {attribute.retired && <Tag>{t.attributes.retired}</Tag>}
                    </span>
                    {attribute.formula !== null && <code className="mt-1 block font-mono text-xs break-all text-ink-muted">{attribute.formula}</code>}
                    {attribute.options.length > 0 && (
                      <span className="mt-1 flex flex-wrap gap-1">
                        {attribute.options.map((option) => (
                          <Tag key={option.id} className={option.retired ? "line-through" : undefined}>
                            {option.name}
                          </Tag>
                        ))}
                      </span>
                    )}
                  </Td>
                  <Td>
                    <span className="text-ink-muted">{kindTitle(attribute.kind)}</span>
                  </Td>
                  <Td>
                    <span className="text-ink-muted">{typeNames(attribute.typeIds)}</span>
                  </Td>
                  {mayChange && (
                    <Td className="text-right whitespace-nowrap">
                      <IconButton
                        icon={<Icon.ChevronDown className="rotate-180" />}
                        label={t.attributes.moveUp(attribute.name)}
                        size="sm"
                        disabled={index === 0}
                        onClick={() => actions.update.mutate({ id: attribute.id, position: index - 1 })}
                      />
                      <IconButton
                        icon={<Icon.ChevronDown />}
                        label={t.attributes.moveDown(attribute.name)}
                        size="sm"
                        disabled={index === attributes.length - 1}
                        onClick={() => actions.update.mutate({ id: attribute.id, position: index + 1 })}
                      />
                      <Button variant="ghost" size="sm" onClick={() => setEditing(attribute.id)} data-action="edit-attribute">
                        {t.attributes.edit}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => actions.update.mutate({ id: attribute.id, retired: !attribute.retired })}
                        data-action={attribute.retired ? "restore-attribute" : "retire-attribute"}
                      >
                        {attribute.retired ? t.attributes.unretire : t.attributes.retire}
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => actions.remove.mutate(attribute.id)} data-action="delete-attribute">
                        {t.attributes.delete}
                      </Button>
                    </Td>
                  )}
                </tr>
              ),
            )}
          </tbody>
        </Table>
      )}
      {mayChange &&
        (adding ? (
          <Card className="p-4">
            <AttributeForm projectKey={project.key} types={types ?? []} onDone={() => setAdding(false)} />
          </Card>
        ) : (
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-attribute">
            {t.attributes.create}
          </Button>
        ))}
    </div>
  );
}

/** Defines an attribute, or changes the one given; its kind is chosen once. */
function AttributeForm({ projectKey, types, attribute, onDone }: { projectKey: string; types: RequirementType[]; attribute?: Attribute; onDone: () => void }) {
  const actions = useAttributeActions(projectKey);
  const { data: kinds } = useAttributeKinds();
  const save = attribute ? actions.update : actions.create;
  const [name, setName] = useState(attribute?.name ?? "");
  const [kind, setKind] = useState<Kind>(attribute?.kind ?? "text");
  // A computed attribute (#213) is made so and stays so; its kind follows from the formula.
  const [computed, setComputed] = useState(attribute ? attribute.formula !== null : false);
  const [formula, setFormula] = useState(attribute?.formula ?? "");
  const [onSections, setOnSections] = useState(attribute?.onSections ?? false);
  const [required, setRequired] = useState(attribute?.required ?? false);
  const [typeIds, setTypeIds] = useState<string[]>(attribute?.typeIds ?? []);
  // Each existing option keeps its id, so a rename is the same option; a new one has none yet.
  const [options, setOptions] = useState<OptionChange[]>(attribute?.options.map((o) => ({ id: o.id, name: o.name, retired: o.retired })) ?? []);
  const [newOption, setNewOption] = useState("");
  // The field rules (#206): from which status on it is required, the select it depends on, and who changes it.
  const { data: statuses } = useStatuses(projectKey);
  const { data: others } = useAttributes(projectKey);
  const [requiredFrom, setRequiredFrom] = useState<string | null>(attribute?.requiredFrom ?? null);
  const [dependsOn, setDependsOn] = useState(attribute?.dependsOn?.attributeId ?? "");
  const [choices, setChoices] = useState<Record<string, string[]>>(attribute?.dependsOn?.choices ?? {});
  const [permits, setPermits] = useState<FieldPermit[]>(attribute?.editableBy ?? []);
  const parent = (others ?? []).find((each) => each.id === dependsOn);
  const named = options.filter((option): option is OptionChange & { id: string } => Boolean(option.id));
  const fields = save.error instanceof ApiError ? save.error.fields : {};

  function addOption() {
    if (!newOption.trim()) return;
    setOptions([...options, { name: newOption.trim() }]);
    setNewOption("");
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    // The rules go along only where they changed, as every other field of the form leaves the rest alone.
    const dependency = hasOptions(kind) && dependsOn ? { attributeId: dependsOn, choices } : null;
    if (computed) {
      const body = { name, typeIds, formula, onSections };
      if (attribute) actions.update.mutate({ id: attribute.id, ...body }, { onSuccess: onDone });
      else actions.create.mutate(body, { onSuccess: onDone });
      return;
    }
    const body = {
      name,
      required,
      typeIds,
      options: hasOptions(kind) ? options : undefined,
      requiredFrom: requiredFrom !== (attribute?.requiredFrom ?? null) ? requiredFrom : undefined,
      editableBy: JSON.stringify(permits) !== JSON.stringify(attribute?.editableBy ?? []) ? permits : undefined,
      dependsOn: JSON.stringify(dependency) !== JSON.stringify(attribute?.dependsOn ?? null) ? dependency : undefined,
    };
    if (attribute) actions.update.mutate({ id: attribute.id, ...body }, { onSuccess: onDone });
    else actions.create.mutate({ ...body, kind }, { onSuccess: onDone });
  }

  return (
    <form onSubmit={submit} className="space-y-3" noValidate data-attribute-form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <div className="grid gap-3 sm:grid-cols-[1fr_12rem]">
        <Field label={t.attributes.name} value={name} onChange={(event) => setName(event.target.value)} error={fields.name} autoFocus required />
        {!computed && (
          <Select
            label={t.attributes.kind}
            value={kind}
            disabled={Boolean(attribute)}
            hint={attribute ? t.attributes.kindFixed : undefined}
            onChange={(event) => setKind(event.target.value as Kind)}
            error={fields.kind}
          >
            {(kinds ?? []).map((each) => (
              <option key={each.kind} value={each.kind}>
                {each.title}
              </option>
            ))}
          </Select>
        )}
      </div>
      <Checkbox
        label={t.attributes.computedLabel}
        checked={computed}
        disabled={Boolean(attribute)}
        onChange={(event) => setComputed(event.target.checked)}
        data-computed-toggle=""
      />
      {computed && (
        <>
          <p className="text-xs text-ink-subtle">{t.attributes.computedHint}</p>
          <FormulaEditor
            projectKey={projectKey}
            attributeId={attribute?.id}
            value={formula}
            onChange={setFormula}
            onSections={onSections}
            error={fields.formula}
          />
          <Checkbox label={t.attributes.onSections} checked={onSections} onChange={(event) => setOnSections(event.target.checked)} />
        </>
      )}
      {!computed && <Checkbox label={t.attributes.requiredLabel} checked={required} onChange={(event) => setRequired(event.target.checked)} />}
      <fieldset className="space-y-1">
        <legend className="text-sm font-medium text-ink-muted">{t.attributes.showsOn}</legend>
        <p className="text-xs text-ink-subtle">{t.attributes.showsOnHint}</p>
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          {types
            .filter((type) => !type.retired || typeIds.includes(type.id))
            .map((type) => (
              <Checkbox
                key={type.id}
                label={type.name}
                checked={typeIds.includes(type.id)}
                onChange={(event) => setTypeIds(event.target.checked ? [...typeIds, type.id] : typeIds.filter((id) => id !== type.id))}
              />
            ))}
        </div>
        {fields.typeIds && <p className="text-sm text-danger">{fields.typeIds}</p>}
      </fieldset>
      {!computed && hasOptions(kind) && (
        <fieldset className="space-y-2" data-option-editor>
          <legend className="text-sm font-medium text-ink-muted">{t.attributes.options}</legend>
          {options.map((option, i) => (
            <div key={option.id ?? `new-${i}`} className="flex items-center gap-2">
              <Input
                controlSize="sm"
                value={option.name}
                aria-label={t.attributes.optionName(i + 1)}
                onChange={(event) => setOptions(options.map((each, j) => (j === i ? { ...each, name: event.target.value } : each)))}
                className={option.retired ? "line-through" : undefined}
              />
              <Checkbox
                label={t.attributes.optionRetired}
                checked={Boolean(option.retired)}
                onChange={(event) => setOptions(options.map((each, j) => (j === i ? { ...each, retired: event.target.checked } : each)))}
              />
            </div>
          ))}
          <div className="flex items-center gap-2">
            <Input
              controlSize="sm"
              value={newOption}
              aria-label={t.attributes.newOption}
              placeholder={t.attributes.newOption}
              onChange={(event) => setNewOption(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  addOption();
                }
              }}
            />
            <Button type="button" size="sm" variant="secondary" onClick={addOption} data-action="add-option">
              {t.attributes.addOption}
            </Button>
          </div>
          {fields.options && <p className="text-sm text-danger">{fields.options}</p>}
        </fieldset>
      )}
      {!computed && (
        <RequiredFrom id="attribute-required-from" statuses={statuses ?? []} value={requiredFrom} onChange={setRequiredFrom} error={fields.requiredFrom} />
      )}
      {!computed && hasOptions(kind) && (
        <fieldset className="space-y-2" data-depends-on>
          <Select
            label={t.fieldRules.dependsOn}
            id="attribute-depends-on"
            value={dependsOn}
            error={fields.dependsOn}
            onChange={(event) => {
              setDependsOn(event.target.value);
              setChoices({});
            }}
          >
            <option value="">{t.fieldRules.dependsOnNone}</option>
            {(others ?? [])
              .filter((each) => each.kind === "select" && each.id !== attribute?.id && !each.retired)
              .map((each) => (
                <option key={each.id} value={each.id}>
                  {each.name}
                </option>
              ))}
          </Select>
          {parent && (
            <>
              <p className="text-xs text-ink-subtle">{t.fieldRules.dependsOnHint}</p>
              {parent.options
                .filter((option) => !option.retired)
                .map((option) => (
                  <fieldset key={option.id} className="flex flex-wrap gap-x-4 gap-y-1" data-choices-for={option.name}>
                    <legend className="text-xs font-medium text-ink-muted">{t.fieldRules.choicesFor(option.name)}</legend>
                    {named.map((own) => {
                      const picked = choices[option.id] ?? [];
                      return (
                        <Checkbox
                          key={own.id}
                          label={own.name}
                          checked={picked.includes(own.id)}
                          onChange={(event) => {
                            const next = event.target.checked ? [...picked, own.id] : picked.filter((each) => each !== own.id);
                            const rest = Object.fromEntries(Object.entries(choices).filter(([key]) => key !== option.id));
                            setChoices(next.length > 0 ? { ...rest, [option.id]: next } : rest);
                          }}
                        />
                      );
                    })}
                  </fieldset>
                ))}
            </>
          )}
        </fieldset>
      )}
      {!computed && <PermitsEditor id="attribute-permits" statuses={statuses ?? []} permits={permits} onChange={setPermits} error={fields.editableBy} />}
      <div className="flex gap-2">
        <Button type="submit" size="sm" loading={save.isPending} data-action="save-attribute">
          {attribute ? t.attributes.save : t.attributes.create}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onDone}>
          {t.attributes.cancel}
        </Button>
      </div>
    </form>
  );
}
