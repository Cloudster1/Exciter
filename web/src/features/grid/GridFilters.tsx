import { useState, type FormEvent, type ReactNode } from "react";
import { useMe } from "@/api/auth";
import type { Attribute, RequirementType } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { PRIORITIES } from "@/api/requirements";
import type { AttributeCondition, Filter, Op } from "@/api/search";
import { STATUS_CATEGORIES, type Status } from "@/api/workflows";
import { Button, Chip, IconButton, Input, SelectInput, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { GRID_MAX_CONDITIONS, SEARCH_QUERY_MAX_LENGTH } from "@/config";
import { LabelCombobox } from "@/features/labels/LabelCombobox";
import { LabelLink } from "@/features/labels/RequirementLabels";
import { t } from "@/i18n";
import { emptyFilter, narrows, OPS_BY_KIND, VALUELESS } from "./filter";

/** The grid's narrowing: words, a choice per field, labels and attribute conditions, each changing the address. */
export function GridFilters({
  project,
  filter,
  types,
  statuses,
  attributes,
  onFilter,
}: {
  project: Project;
  filter: Filter;
  types: RequirementType[];
  statuses: Status[];
  attributes: Attribute[];
  onFilter: (next: Filter) => void;
}) {
  const { data: me } = useMe();
  const [words, setWords] = useState(filter.q);
  const [seenQ, setSeenQ] = useState(filter.q);
  // A view or Back changes the words under the box, and the box follows.
  if (seenQ !== filter.q) {
    setSeenQ(filter.q);
    setWords(filter.q);
  }
  const set = (patch: Partial<Filter>) => onFilter({ ...filter, ...patch });
  const one = <T extends string>(value: string): T[] => (value ? [value as T] : []);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    set({ q: words.trim() });
  };

  return (
    <section aria-label={t.grid.filters} className="mb-4 space-y-3" data-grid-filters={project.key}>
      <div className="flex flex-wrap items-end gap-2">
        {/* biome-ignore lint/a11y/useSemanticElements: jsdom, which the unit tests run in, does not know the search element yet */}
        <form role="search" onSubmit={submit} className="flex items-end gap-2">
          <Input
            aria-label={t.grid.words}
            type="search"
            value={words}
            onChange={(e) => setWords(e.target.value)}
            placeholder={t.grid.wordsPlaceholder}
            maxLength={SEARCH_QUERY_MAX_LENGTH}
            className="w-56"
            data-grid-words
          />
          <Button type="submit" variant="secondary" icon={<Icon.Search />} data-action="apply-words">
            {t.grid.apply}
          </Button>
        </form>
        <Choice label={t.grid.kind} value={filter.kinds[0] ?? ""} onChange={(v) => set({ kinds: one(v) })} name="kind">
          <option value="">{t.grid.anyKind}</option>
          <option value="requirement">{t.requirements.kinds.requirement}</option>
          <option value="section">{t.requirements.kinds.section}</option>
        </Choice>
        <Choice label={t.grid.type} value={filter.types[0] ?? ""} onChange={(v) => set({ types: one(v) })} name="type">
          <option value="">{t.grid.anyType}</option>
          {types.map((type) => (
            <option key={type.id} value={type.id}>
              {type.name}
            </option>
          ))}
        </Choice>
        <Choice label={t.grid.status} value={filter.statuses[0] ?? ""} onChange={(v) => set({ statuses: one(v) })} name="status">
          <option value="">{t.grid.anyStatus}</option>
          {statuses.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </Choice>
        <Choice label={t.grid.category} value={filter.categories[0] ?? ""} onChange={(v) => set({ categories: one(v) })} name="category">
          <option value="">{t.grid.anyCategory}</option>
          {STATUS_CATEGORIES.map((category) => (
            <option key={category} value={category}>
              {t.workflows.categories[category]}
            </option>
          ))}
        </Choice>
        <Choice label={t.grid.priority} value={filter.priorities[0] ?? ""} onChange={(v) => set({ priorities: one(v) })} name="priority">
          <option value="">{t.grid.anyPriority}</option>
          {PRIORITIES.map((priority) => (
            <option key={priority} value={priority}>
              {t.requirements.priorities[priority]}
            </option>
          ))}
        </Choice>
        <Choice label={t.grid.owner} value={filter.owners[0] ?? ""} onChange={(v) => set({ owners: one(v) })} name="owner">
          <option value="">{t.grid.anyone}</option>
          {me && <option value={me.user.id}>{t.grid.ownedByMe}</option>}
          {filter.owners[0] && filter.owners[0] !== me?.user.id && <option value={filter.owners[0]}>{filter.owners[0]}</option>}
        </Choice>
        <Chip pressed={filter.suspect} onClick={() => set({ suspect: !filter.suspect })} data-filter="suspect">
          <Icon.Warning className="size-3.5" />
          {t.suspect.onlySuspect}
        </Chip>
        <div className="w-48" data-filter="label">
          <LabelCombobox
            label={t.grid.labels}
            exclude={filter.labels}
            newOption={t.grid.labelFilter}
            onPick={(name) => set({ labels: [...filter.labels, name] })}
            onRemoveLast={() => set({ labels: filter.labels.slice(0, -1) })}
          />
        </div>
        {narrows(filter) && (
          <Button variant="link" onClick={() => onFilter(emptyFilter())} data-action="clear-grid-filters">
            {t.grid.clear}
          </Button>
        )}
      </div>
      {filter.labels.length > 0 && (
        <ul className="flex flex-wrap gap-1.5" aria-label={t.grid.labels} data-grid-label-filters>
          {filter.labels.map((name) => (
            <li key={name}>
              <LabelLink name={name} projectKey={project.key} onRemove={() => set({ labels: filter.labels.filter((each) => each !== name) })} />
            </li>
          ))}
        </ul>
      )}
      <Conditions conditions={filter.attributes} attributes={attributes} me={me?.user} onChange={(next) => set({ attributes: next })} />
    </section>
  );
}

function Choice({
  label,
  value,
  onChange,
  name,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  name: string;
  children: ReactNode;
}) {
  return (
    <SelectInput aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} className="w-auto" data-grid-filter={name}>
      {children}
    </SelectInput>
  );
}

/** How a condition reads: the attribute, the comparison and the value as the reader knows it. */
export function describeCondition(c: AttributeCondition, attributes: Attribute[]): string {
  const attribute = attributes.find((each) => each.id === c.attributeId);
  const name = attribute?.name ?? c.attributeId;
  const op = t.grid.ops[c.op] ?? c.op;
  if (VALUELESS.includes(c.op)) return `${name} ${op}`;
  const option = attribute?.options.find((each) => each.id === c.value);
  const value = option?.name ?? (c.value === "true" ? t.grid.yes : c.value === "false" ? t.grid.no : c.value);
  return `${name} ${op} ${value}`;
}

function Conditions({
  conditions,
  attributes,
  me,
  onChange,
}: {
  conditions: AttributeCondition[];
  attributes: Attribute[];
  me?: Person;
  onChange: (next: AttributeCondition[]) => void;
}) {
  const offered = attributes.filter((each) => !each.retired);
  const [attributeId, setAttributeId] = useState("");
  const [op, setOp] = useState<Op>("is");
  const [value, setValue] = useState("");
  const attribute = offered.find((each) => each.id === attributeId);
  const ops = attribute ? OPS_BY_KIND[attribute.kind] : [];
  const valueless = VALUELESS.includes(op);
  const ready = attribute && ops.includes(op) && (valueless || value.trim() !== "") && conditions.length < GRID_MAX_CONDITIONS;
  const add = (event: FormEvent) => {
    event.preventDefault();
    if (!ready) return;
    onChange([...conditions, { attributeId, op, value: valueless ? "" : value.trim() }]);
    setValue("");
  };
  if (offered.length === 0 && conditions.length === 0) return null;

  return (
    <div className="space-y-2" data-grid-conditions>
      {conditions.length > 0 && (
        <ul className="flex flex-wrap gap-1.5" aria-label={t.grid.conditions}>
          {conditions.map((c, index) => {
            const text = describeCondition(c, attributes);
            return (
              <li key={`${c.attributeId}:${c.op}:${c.value}`}>
                <Tag className="inline-flex items-center gap-1" data-grid-condition={text}>
                  {text}
                  <IconButton
                    icon={<Icon.X className="size-3" />}
                    label={t.grid.removeCondition(text)}
                    size="xs"
                    onClick={() => onChange(conditions.filter((_, i) => i !== index))}
                    data-action="remove-condition"
                  />
                </Tag>
              </li>
            );
          })}
        </ul>
      )}
      {offered.length > 0 && (
        <form onSubmit={add} className="flex flex-wrap items-end gap-2" aria-label={t.grid.conditions}>
          <SelectInput
            aria-label={t.grid.attribute}
            value={attributeId}
            onChange={(e) => {
              setAttributeId(e.target.value);
              setOp("is");
              setValue("");
            }}
            className="w-auto"
            data-condition-attribute
          >
            <option value="">{t.grid.attribute}</option>
            {offered.map((each) => (
              <option key={each.id} value={each.id}>
                {each.name}
              </option>
            ))}
          </SelectInput>
          {attribute && (
            <>
              <SelectInput aria-label={t.grid.op} value={op} onChange={(e) => setOp(e.target.value as Op)} className="w-auto" data-condition-op>
                {ops.map((each) => (
                  <option key={each} value={each}>
                    {t.grid.ops[each]}
                  </option>
                ))}
              </SelectInput>
              {!valueless && <ValueInput attribute={attribute} value={value} me={me} onChange={setValue} />}
            </>
          )}
          <Button type="submit" variant="secondary" icon={<Icon.Plus />} disabled={!ready} data-action="add-condition">
            {t.grid.addCondition}
          </Button>
        </form>
      )}
    </div>
  );
}

/** Who the reader is, which a person attribute is compared with. */
interface Person {
  id: string;
  name: string;
}

function ValueInput({ attribute, value, me, onChange }: { attribute: Attribute; value: string; me?: Person; onChange: (value: string) => void }) {
  const common = { "aria-label": t.grid.value, value, "data-condition-value": "" };
  switch (attribute.kind) {
    case "select":
    case "multiSelect":
      return (
        <SelectInput {...common} onChange={(e) => onChange(e.target.value)} className="w-auto">
          <option value="">{t.grid.pickValue}</option>
          {attribute.options.map((option) => (
            <option key={option.id} value={option.id}>
              {option.name}
            </option>
          ))}
        </SelectInput>
      );
    case "boolean":
      return (
        <SelectInput {...common} onChange={(e) => onChange(e.target.value)} className="w-auto">
          <option value="">{t.grid.pickValue}</option>
          <option value="true">{t.grid.yes}</option>
          <option value="false">{t.grid.no}</option>
        </SelectInput>
      );
    case "user":
      return (
        <SelectInput {...common} onChange={(e) => onChange(e.target.value)} className="w-auto">
          <option value="">{t.grid.pickValue}</option>
          {me && <option value={me.id}>{t.search.you(me.name)}</option>}
        </SelectInput>
      );
    case "number":
      return <Input {...common} type="number" step="any" onChange={(e) => onChange(e.target.value)} className="w-28" />;
    case "date":
      return <Input {...common} type="date" onChange={(e) => onChange(e.target.value)} className="w-40" />;
    default:
      return <Input {...common} onChange={(e) => onChange(e.target.value)} className="w-48" />;
  }
}
