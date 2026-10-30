import { Link } from "@tanstack/react-router";
import { useAttributes, type Attribute } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { useUpdateRequirement } from "@/api/requirements";
import { useRefreshRisks, type Dimension, type Rating, type RiskRow, type RiskScheme } from "@/api/risks";
import { SelectInput, Table, Tag, Td, Th } from "@/components/ui";
import { AttributeValues, shownAttributes } from "@/features/types/AttributeFields";
import { t } from "@/i18n";
import { LevelTag } from "./LevelTag";
import { StateTag } from "./StateTag";

const SIDES = ["before", "after"] as const;
type SideName = (typeof SIDES)[number];

/** The FMEA table (#260): a row a risk, its rating before and after mitigation edited in place where the reader may,
 * its mitigations between, and the state and level it stands at. */
export function FmeaTable({ project, rows, scheme }: { project: Project; rows: RiskRow[]; scheme: RiskScheme | null }) {
  const { data: attributes } = useAttributes(project.key);
  const dimensions: Dimension[] = scheme?.scales.map((s) => s.dimension) ?? ["severity", "probability"];
  return (
    <Table aria-label={t.risks.fmeaTitle} dense data-fmea-table="">
      <thead>
        <tr>
          <Th>{t.risks.columns.risk}</Th>
          <Th colSpan={dimensions.length + 2}>{t.risks.columns.before}</Th>
          <Th>{t.risks.columns.mitigatedBy}</Th>
          <Th colSpan={dimensions.length + 2}>{t.risks.columns.after}</Th>
          <Th>{t.risks.columns.state}</Th>
          <Th>{t.risks.columns.level}</Th>
        </tr>
        <tr>
          <Th>
            <span className="sr-only">{t.risks.columns.risk}</span>
          </Th>
          {SIDES.map((side) => (
            <SideHeadings key={side} side={side} dimensions={dimensions} scheme={scheme} mitigations={side === "after"} />
          ))}
          <Th>
            <span className="sr-only">{t.risks.columns.state}</span>
          </Th>
          <Th>
            <span className="sr-only">{t.risks.columns.level}</span>
          </Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <FmeaRow key={row.risk.id} project={project} row={row} scheme={scheme} dimensions={dimensions} attributes={attributes ?? []} />
        ))}
      </tbody>
    </Table>
  );
}

function SideHeadings({ side, dimensions, scheme, mitigations }: { side: SideName; dimensions: Dimension[]; scheme: RiskScheme | null; mitigations: boolean }) {
  return (
    <>
      {mitigations && (
        <Th>
          <span className="sr-only">{t.risks.columns.mitigatedBy}</span>
        </Th>
      )}
      {dimensions.map((d) => (
        <Th key={`${side}-${d}`}>{scheme?.scales.find((s) => s.dimension === d)?.label ?? t.risks.dimensions[d]}</Th>
      ))}
      <Th>{t.risks.rpn}</Th>
      <Th>{t.risks.level}</Th>
    </>
  );
}

function FmeaRow({
  project,
  row,
  scheme,
  dimensions,
  attributes,
}: {
  project: Project;
  row: RiskRow;
  scheme: RiskScheme | null;
  dimensions: Dimension[];
  attributes: Attribute[];
}) {
  const update = useUpdateRequirement(row.risk.key);
  const refresh = useRefreshRisks(project.key);
  const rating: Rating = { before: row.rating.before, after: row.rating.after };
  const editable = row.can.edit && scheme !== null;
  function pick(side: SideName, dimension: Dimension, value: string) {
    const next: Rating = { ...rating, [side]: { ...rating[side], [dimension]: value === "" ? null : Number(value) } };
    update.mutate({ risk: next, baseRevision: row.risk.revision }, { onSettled: () => void refresh() });
  }
  const sideCells = (side: SideName) => (
    <>
      {dimensions.map((d) => (
        <Td key={`${side}-${d}`}>
          {editable ? (
            <SelectInput
              aria-label={t.risks.edit(d, side, row.risk.key)}
              controlSize="sm"
              value={rating[side][d] ?? ""}
              disabled={update.isPending}
              onChange={(event) => pick(side, d, event.target.value)}
            >
              <option value="">-</option>
              {scheme!.scales
                .find((s) => s.dimension === d)
                ?.values.map((v) => (
                  <option key={v.value} value={v.value} title={v.name}>
                    {v.value}
                  </option>
                ))}
            </SelectInput>
          ) : (
            (rating[side][d] ?? "-")
          )}
        </Td>
      ))}
      <Td>{(side === "before" ? row.rating.beforeRpn : row.rating.afterRpn) ?? "-"}</Td>
      <Td>
        <LevelTag level={side === "before" ? row.rating.beforeLevel : row.rating.afterLevel} />
      </Td>
    </>
  );
  return (
    <tr data-fmea-row={row.risk.key}>
      <Td className="min-w-48 align-top">
        <Link to="/projects/$projectKey/r/$reqKey" params={{ projectKey: project.key, reqKey: row.risk.key }} className="font-medium text-accent">
          {row.risk.key}
        </Link>{" "}
        <span className="text-ink">{row.risk.title}</span>
        <span className="block text-xs text-ink-subtle">{row.risk.type.name}</span>
        <AttributeValues attributes={shownAttributes(attributes, row.risk.type.id, row.attributes)} values={row.attributes} />
        {update.error && (
          <p role="alert" className="text-xs text-danger">
            {t.risks.saveFailed(row.risk.key, update.error.message)}
          </p>
        )}
      </Td>
      {sideCells("before")}
      <Td className="align-top">
        <ul className="space-y-1">
          {row.mitigations.map((m) => (
            <li key={m.linkId} className="flex flex-wrap items-center gap-1" data-mitigation={m.source.key || "hidden"}>
              {m.source.hidden ? (
                <span className="text-ink-muted">{t.risks.hidden}</span>
              ) : (
                <Link to="/projects/$projectKey/r/$reqKey" params={{ projectKey: m.source.projectKey, reqKey: m.source.key }} className="text-accent">
                  {m.source.key}
                </Link>
              )}
              <Tag>{m.inEffect ? t.risks.inEffect : t.risks.notInEffect}</Tag>
            </li>
          ))}
        </ul>
      </Td>
      {sideCells("after")}
      <Td>
        <StateTag state={row.rating.state} />
      </Td>
      <Td>
        <LevelTag level={row.rating.level} />
      </Td>
    </tr>
  );
}
