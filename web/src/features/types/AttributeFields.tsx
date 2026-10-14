import { useQuery } from "@tanstack/react-query";
import { showsOn, type Attribute } from "@/api/attributes";
import { api } from "@/api/client";
import { findPeople } from "@/api/requirements";
import { Checkbox, IconButton, Input, Labelled, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MAX_PICKER_LIMIT } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import type { Doc } from "@/features/editor/schema";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";

export type Values = Record<string, unknown>;

/** The attributes a requirement of a type shows: those that show on it, a retired one only while it holds a value. */
export function shownAttributes(attributes: Attribute[], typeId: string | null | undefined, values: Values): Attribute[] {
  return attributes.filter((a) => showsOn(a, typeId) && (!a.retired || values[a.id] !== undefined));
}

/** The values a requirement keeps on taking another type: those of attributes that show on it, as the server keeps them. */
export function valuesFor(attributes: Attribute[], typeId: string | null | undefined, values: Values): Values {
  return Object.fromEntries(
    Object.entries(values).map(([id, value]) => {
      const attribute = attributes.find((a) => a.id === id);
      return [id, attribute && showsOn(attribute, typeId) ? value : null];
    }),
  );
}

/** The names of the installation's members by id, for showing a person a value names. */
export function usePeopleNames() {
  return useQuery({
    queryKey: ["people", "names"],
    queryFn: async () => {
      const people = (await api.GET("/people", { params: { query: { limit: MAX_PICKER_LIMIT } } })).data?.people ?? [];
      return new Map(people.map((p) => [p.id, p.name || p.email]));
    },
  });
}

/** A requirement's attribute values as a form: each attribute the control its kind asks for. */
export function AttributeFields({
  attributes,
  typeId,
  values,
  onChange,
  errors,
}: {
  attributes: Attribute[];
  typeId: string | null | undefined;
  values: Values;
  onChange: (values: Values) => void;
  errors: Record<string, string>;
}) {
  const { data: names } = usePeopleNames();
  const set = (id: string, value: unknown) => onChange({ ...values, [id]: value });
  const shown = shownAttributes(attributes, typeId, values);
  if (shown.length === 0) return null;
  return (
    <fieldset className="space-y-3" data-attribute-fields>
      <legend className="text-sm font-semibold text-ink">{t.attributes.valuesTitle}</legend>
      {shown.map((a) => {
        const id = `attribute-${a.id}`;
        const value = values[a.id];
        const error = errors[`attributes.${a.id}`];
        const label = a.required ? `${a.name} (${t.attributes.required.toLowerCase()})` : a.name;
        if (a.kind === "boolean") {
          return (
            <div key={a.id} data-attribute-field={a.name}>
              <Checkbox label={a.name} checked={value === true} onChange={(event) => set(a.id, event.target.checked)} />
              {error && <p className="text-sm text-danger">{error}</p>}
            </div>
          );
        }
        return (
          <Labelled key={a.id} id={id} label={label} error={error}>
            <div data-attribute-field={a.name}>
              {a.kind === "richText" ? (
                <Editor id={id} value={(value as Doc | null) ?? null} onChange={(doc) => set(a.id, doc)} mentions={findPeople} aria-label={a.name} />
              ) : a.kind === "select" ? (
                <SelectInput id={id} value={(value as string) ?? ""} onChange={(event) => set(a.id, event.target.value || null)}>
                  <option value="">{t.attributes.none}</option>
                  {a.options
                    .filter((o) => !o.retired || o.id === value)
                    .map((o) => (
                      <option key={o.id} value={o.id}>
                        {o.name}
                      </option>
                    ))}
                </SelectInput>
              ) : a.kind === "multiSelect" ? (
                <div className="flex flex-wrap gap-x-4 gap-y-1" id={id}>
                  {a.options
                    .filter((o) => !o.retired || ((value as string[]) ?? []).includes(o.id))
                    .map((o) => {
                      const picked = ((value as string[] | undefined) ?? []).includes(o.id);
                      return (
                        <Checkbox
                          key={o.id}
                          label={o.name}
                          checked={picked}
                          onChange={(event) => {
                            const was = (value as string[] | undefined) ?? [];
                            set(a.id, event.target.checked ? [...was, o.id] : was.filter((each) => each !== o.id));
                          }}
                        />
                      );
                    })}
                </div>
              ) : a.kind === "user" ? (
                <div className="flex items-center gap-2">
                  <span className="text-sm text-ink">{value ? (names?.get(value as string) ?? "") : t.attributes.none}</span>
                  {Boolean(value) && <IconButton icon={<Icon.X />} label={t.attributes.clear(a.name)} size="xs" onClick={() => set(a.id, null)} />}
                  <SubjectPicker label={t.attributes.pickPerson(a.name)} peopleOnly onPick={(person) => set(a.id, person.id)} />
                </div>
              ) : (
                <Input
                  id={id}
                  type={a.kind === "number" ? "number" : a.kind === "date" ? "date" : a.kind === "url" ? "url" : "text"}
                  step={a.kind === "number" ? "any" : undefined}
                  placeholder={a.kind === "url" ? "https://" : undefined}
                  value={value === undefined || value === null ? "" : String(value)}
                  invalid={Boolean(error)}
                  onChange={(event) => {
                    const raw = event.target.value;
                    set(a.id, raw === "" ? null : a.kind === "number" ? Number(raw) : raw);
                  }}
                />
              )}
            </div>
          </Labelled>
        );
      })}
    </fieldset>
  );
}

/** A requirement's attribute values as a reader sees them: options and people by name, nothing for what is empty. */
export function AttributeValues({ attributes, values }: { attributes: Attribute[]; values: Values }) {
  const { data: names } = usePeopleNames();
  const given = attributes.filter((a) => values[a.id] !== undefined && values[a.id] !== null);
  if (given.length === 0) return null;
  const optionName = (a: Attribute, id: unknown) => a.options.find((o) => o.id === id)?.name ?? String(id);
  return (
    <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[minmax(8rem,auto)_1fr]" data-attribute-values>
      {given.map((a) => {
        const value = values[a.id];
        return (
          <div key={a.id} className="contents" data-attribute-value={a.name}>
            <dt className="text-ink-muted">{a.name}</dt>
            <dd className="min-w-0 text-ink">
              {a.kind === "richText" ? (
                <DocView doc={value as Doc} />
              ) : a.kind === "select" ? (
                optionName(a, value)
              ) : a.kind === "multiSelect" ? (
                (value as string[]).map((id) => optionName(a, id)).join(", ")
              ) : a.kind === "boolean" ? (
                value ? (
                  t.attributes.yes
                ) : (
                  t.attributes.no
                )
              ) : a.kind === "user" ? (
                (names?.get(value as string) ?? "")
              ) : a.kind === "url" ? (
                <a href={String(value)} target="_blank" rel="noopener noreferrer" className="break-all text-accent hover:underline">
                  {String(value)}
                </a>
              ) : (
                String(value)
              )}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}
