import { useQuery } from "@tanstack/react-query";
import { allowedOptions, showsOn, type Attribute } from "@/api/attributes";
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
  readOnly = {},
  computed,
}: {
  attributes: Attribute[];
  typeId: string | null | undefined;
  values: Values;
  onChange: (values: Values) => void;
  errors: Record<string, string>;
  /** Attributes the reader may not change here, by id, with why (#206). */
  readOnly?: Record<string, string>;
  /** The requirement's computed values (#213), shown beside the fields and never typed. */
  computed?: Values;
}) {
  const { data: names } = usePeopleNames();
  // A change of an attribute another depends on clears the other's options it no longer allows (#206).
  const set = (id: string, value: unknown) => {
    const next: Values = { ...values, [id]: value };
    for (const a of attributes) {
      if (a.dependsOn?.attributeId !== id || next[a.id] === undefined || next[a.id] === null) continue;
      const allowed = allowedOptions(a, next).map((o) => o.id);
      const held = next[a.id];
      if (Array.isArray(held)) next[a.id] = held.filter((each) => allowed.includes(each as string));
      else if (!allowed.includes(held as string)) next[a.id] = null;
    }
    onChange(next);
  };
  const shown = shownAttributes(attributes, typeId, values).filter((a) => a.formula === null);
  const worked = attributes.filter((a) => a.formula !== null);
  if (shown.length === 0 && !(computed && worked.length > 0)) return null;
  return (
    <fieldset className="space-y-3" data-attribute-fields>
      <legend className="text-sm font-semibold text-ink">{t.attributes.valuesTitle}</legend>
      {computed && <AttributeValues attributes={worked} values={computed} />}
      {shown.map((a) => {
        const id = `attribute-${a.id}`;
        const value = values[a.id];
        const error = errors[`attributes.${a.id}`];
        const label = a.required ? `${a.name} (${t.attributes.required.toLowerCase()})` : a.name;
        const options = allowedOptions(a, values);
        if (readOnly[a.id] !== undefined) {
          return (
            <div key={a.id} className="space-y-1" data-attribute-field={a.name} data-read-only-why={a.id}>
              <AttributeValues attributes={[a]} values={values} />
              {(value === undefined || value === null) && (
                <p className="text-sm text-ink">
                  {a.name}: {t.attributes.none}
                </p>
              )}
              <p className="text-xs text-ink-muted">{readOnly[a.id]}</p>
              {error && <p className="text-sm text-danger">{error}</p>}
            </div>
          );
        }
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
                  {options
                    .filter((o) => !o.retired || o.id === value)
                    .map((o) => (
                      <option key={o.id} value={o.id}>
                        {o.name}
                      </option>
                    ))}
                </SelectInput>
              ) : a.kind === "multiSelect" ? (
                <div className="flex flex-wrap gap-x-4 gap-y-1" id={id}>
                  {options
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
            <dt className="text-ink-muted">
              {a.name}
              {a.formula !== null && <span className="ml-1.5 text-xs text-ink-subtle">({t.attributes.computed})</span>}
            </dt>
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
