import { useAttributeKinds, useAttributes, useTypes } from "@/api/attributes";
import { useProjectLinkTypes } from "@/api/links";
import { CHOICE_CREATE, CHOICE_SKIP, type ReqIFSchemaItem } from "@/api/reqif";
import { Card, SelectInput, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";

/** What the file's types, attributes and link types become in the project, each one open to a choice. */
export function SchemaCard({
  projectKey,
  items,
  choices,
  onChange,
}: {
  projectKey: string;
  items: ReqIFSchemaItem[];
  choices: Record<string, string>;
  onChange: (choices: Record<string, string>) => void;
}) {
  const { data: types = [] } = useTypes(projectKey);
  const { data: attributes = [] } = useAttributes(projectKey);
  const { data: linkTypes = [] } = useProjectLinkTypes(projectKey);
  const { data: kinds = [] } = useAttributeKinds();
  if (items.length === 0) return null;
  const kindTitle = (kind?: string) => kinds.find((each) => each.kind === kind)?.title ?? kind ?? "";

  function options(item: ReqIFSchemaItem): { value: string; label: string }[] {
    switch (item.kind) {
      case "type":
        return [
          { value: CHOICE_CREATE, label: t.reqif.makeType },
          ...types.filter((each) => !each.retired).map((each) => ({ value: each.id, label: each.name })),
        ];
      case "linkType":
        return [
          { value: CHOICE_CREATE, label: t.reqif.makeLinkType },
          ...linkTypes.filter((each) => each.enabled && !each.retired).map((each) => ({ value: each.id, label: each.name })),
        ];
      default:
        return [
          { value: CHOICE_CREATE, label: t.reqif.makeAttribute(kindTitle(item.fits?.[0])) },
          { value: CHOICE_SKIP, label: t.reqif.skip },
          ...attributes
            .filter((each) => !each.retired && (item.fits ?? []).includes(each.kind))
            .map((each) => ({ value: each.id, label: `${each.name} (${kindTitle(each.kind)})` })),
        ];
    }
  }

  function current(item: ReqIFSchemaItem): string {
    const chosen = choices[item.identifier];
    if (chosen) return chosen;
    if (item.means) return item.means;
    if (item.by === "create") return CHOICE_CREATE;
    if (item.by === "skip") return CHOICE_SKIP;
    return "";
  }

  return (
    <Card className="p-4" data-reqif-schema>
      <h3 className="mb-1 text-sm font-medium text-ink">{t.reqif.schemaTitle}</h3>
      <p className="mb-3 text-sm text-ink-muted">{t.reqif.schemaIntro}</p>
      <Table dense aria-label={t.reqif.schemaTitle}>
        <thead>
          <tr>
            <Th>{t.reqif.kind}</Th>
            <Th>{t.reqif.inFile}</Th>
            <Th>{t.reqif.used}</Th>
            <Th>{t.reqif.becomes}</Th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.identifier} data-reqif-item={item.name} data-reqif-by={item.by}>
              <Td className="text-sm text-ink-muted">{t.reqif.kinds[item.kind]}</Td>
              <Td className="text-sm text-ink">{item.name}</Td>
              <Td className="text-sm text-ink-muted tabular-nums">{item.count}</Td>
              <Td>{fixed(item) ?? <Chooser item={item} value={current(item)} options={options(item)} onPick={(value) => onChange({ ...choices, [item.identifier]: value })} />}</Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </Card>
  );
}

// What an item becomes when there is nothing to choose: a field, a heading's type or attribute.
function fixed(item: ReqIFSchemaItem): string | undefined {
  if (item.by === "field") return t.reqif.fields[item.field ?? ""] ?? item.field;
  if (item.by === "section") return item.kind === "attribute" ? t.reqif.headingAttribute : t.reqif.sections;
  return undefined;
}

function Chooser({
  item,
  value,
  options,
  onPick,
}: {
  item: ReqIFSchemaItem;
  value: string;
  options: { value: string; label: string }[];
  onPick: (value: string) => void;
}) {
  return (
    <SelectInput
      aria-label={t.reqif.whatBecomes(item.name)}
      controlSize="sm"
      value={value}
      onChange={(event) => onPick(event.target.value)}
      data-reqif-becomes={item.name}
    >
      {value === "" && <option value="">{t.reqif.pick}</option>}
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </SelectInput>
  );
}
