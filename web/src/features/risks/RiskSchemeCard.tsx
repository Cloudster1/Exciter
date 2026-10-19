import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useRiskScheme, useSaveRiskScheme, type Dimension, type RiskScheme, type RiskSchemeInput } from "@/api/risks";
import {
  Button,
  Card,
  Checkbox,
  ColorField,
  ErrorBanner,
  Field,
  IconButton,
  SectionTitle,
  Select,
  SelectInput,
  Skeleton,
  Table,
  Td,
  Th,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { RISK_DEFAULT_LEVELS, RISK_DEFAULT_SCALE, RISK_MAX_LEVELS, RISK_MAX_SCALE_VALUE, TYPE_COLOURS } from "@/config";
import { t } from "@/i18n";
import { defaultScheme } from "./level";

type Draft = RiskSchemeInput;
type Scale = Draft["scales"][number];
type Level = Draft["levels"][number];

/** A project's risk scheme (#259): its administrators set the scales, the levels and how a rating gives its level. */
export function RiskSchemeCard({ project }: { project: Project }) {
  const { data, error, refetch } = useRiskScheme(project.key);
  // A saved scheme reads afresh into a new form, so the notice that it is saved lives here.
  const [notice, setNotice] = useState("");
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (data === undefined) return <Skeleton />;
  return <SchemeForm key={data?.updatedAt ?? "none"} project={project} scheme={data} notice={notice} setNotice={setNotice} />;
}

/** The scheme as the form edits it. */
function draftOf(scheme: RiskScheme): Draft {
  return {
    method: scheme.method,
    detection: scheme.detection,
    scales: scheme.scales.map(({ dimension, label, values }) => ({ dimension, label, values })),
    levels: scheme.levels,
    matrix: scheme.matrix,
  };
}

function scaleOf(draft: Draft, dimension: Dimension): Scale | undefined {
  return draft.scales.find((s) => s.dimension === dimension);
}

/** A scale of n values named as the settings name them first. */
function freshScale(dimension: Dimension, n: number): Scale {
  return {
    dimension,
    label: t.risks.dimensions[dimension],
    values: Array.from({ length: n }, (_, i) => ({ value: i + 1, name: t.risks.valueName(dimension, i + 1), description: "" })),
  };
}

/** Every cell the scales make, each keeping the level it had while that level is still there, the first otherwise. */
function settleMatrix(draft: Draft): Draft {
  if (draft.method !== "matrix") return { ...draft, matrix: [] };
  const severities = scaleOf(draft, "severity")?.values.length ?? 0;
  const probabilities = scaleOf(draft, "probability")?.values.length ?? 0;
  const names = draft.levels.map((l) => l.name);
  const matrix = [];
  for (let severity = 1; severity <= severities; severity++) {
    for (let probability = 1; probability <= probabilities; probability++) {
      const had = draft.matrix.find((c) => c.severity === severity && c.probability === probability)?.level;
      matrix.push({ severity, probability, level: had && names.includes(had) ? had : (names[0] ?? "") });
    }
  }
  return { ...draft, matrix };
}

function SchemeForm({
  project,
  scheme,
  notice,
  setNotice,
}: {
  project: Project;
  scheme: RiskScheme | null;
  notice: string;
  setNotice: (notice: string) => void;
}) {
  const may = project.can.administer && !project.archivedAt;
  const save = useSaveRiskScheme(project.key);
  const [draft, setDraft] = useState<Draft | null>(scheme ? draftOf(scheme) : null);
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const held = (dimension: Dimension) => new Set(scheme?.scales.find((s) => s.dimension === dimension)?.held ?? []);
  const change = (next: Draft) => {
    setNotice("");
    setDraft(settleMatrix(next));
  };

  if (!draft) {
    return (
      <Card className="space-y-3 p-4" data-risk-scheme="">
        <SectionTitle>{t.risks.schemeTitle}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.risks.noScheme}</p>
        {may ? (
          <Button onClick={() => change(defaultScheme())} data-action="start-risk-scheme">
            {t.risks.startFromDefault}
          </Button>
        ) : (
          <p className="text-sm text-ink-muted">{t.risks.readOnly}</p>
        )}
      </Card>
    );
  }

  function setScale(dimension: Dimension, next: Scale) {
    change({ ...draft!, scales: draft!.scales.map((s) => (s.dimension === dimension ? next : s)) });
  }
  function setDetection(on: boolean) {
    const scales = draft!.scales.filter((s) => s.dimension !== "detection");
    change({ ...draft!, detection: on, scales: on ? [...scales, freshScale("detection", RISK_DEFAULT_SCALE)] : scales });
  }
  function setMethod(method: Draft["method"]) {
    // A priority number needs a threshold for every level but the first, which starts from the defaults.
    const levels = draft!.levels.map((l, i) => (method === "rpn" && i > 0 && l.minRpn == null ? { ...l, minRpn: RISK_DEFAULT_LEVELS[i]?.from ?? i + 1 } : l));
    change({ ...draft!, method, levels });
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    const body = { ...draft!, levels: draft!.levels.map((l) => ({ ...l, colour: l.colour.toLowerCase() })) };
    save.mutate(body, { onSuccess: () => setNotice(t.risks.saved) });
  }

  return (
    <Card className="space-y-4 p-4" data-risk-scheme="">
      <SectionTitle>{t.risks.schemeTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.risks.schemeIntro}</p>
      {!may && <p className="text-sm text-ink-muted">{t.risks.readOnly}</p>}
      <form onSubmit={submit} className="space-y-5" aria-label={t.risks.schemeTitle} noValidate>
        <fieldset disabled={!may} className="space-y-5">
          <div className="grid gap-3 sm:grid-cols-2">
            <Select label={t.risks.method} value={draft.method} onChange={(event) => setMethod(event.target.value as Draft["method"])} error={fields.method}>
              <option value="matrix">{t.risks.methods.matrix}</option>
              <option value="rpn">{t.risks.methods.rpn}</option>
            </Select>
            <div className="space-y-1 self-end">
              <Checkbox
                label={t.risks.detection}
                checked={draft.detection}
                onChange={(event) => setDetection(event.target.checked)}
                aria-describedby="risk-detection-hint"
              />
              <p id="risk-detection-hint" className="text-xs text-ink-muted">
                {t.risks.detectionHint}
              </p>
            </div>
          </div>
          {draft.scales.map((scale, si) => (
            <ScaleFields
              key={scale.dimension}
              scale={scale}
              index={si}
              held={held(scale.dimension)}
              fields={fields}
              onChange={(next) => setScale(scale.dimension, next)}
            />
          ))}
          <LevelFields draft={draft} fields={fields} onChange={change} />
          {draft.method === "matrix" && <MatrixEditor draft={draft} onChange={change} />}
        </fieldset>
        {save.error && <ErrorBanner>{Object.keys(fields).length > 0 ? Object.values(fields).join(" ") : save.error.message}</ErrorBanner>}
        {may && (
          <Button type="submit" loading={save.isPending} data-action="save-risk-scheme">
            {t.risks.save}
          </Button>
        )}
        <p role="status" className="text-sm text-ink-muted empty:hidden">
          {notice}
        </p>
      </form>
    </Card>
  );
}

/** One scale: what the project calls it, and its values from 1 up, each named. */
function ScaleFields({
  scale,
  index,
  held,
  fields,
  onChange,
}: {
  scale: Scale;
  index: number;
  held: Set<number>;
  fields: Record<string, string>;
  onChange: (next: Scale) => void;
}) {
  const highest = scale.values[scale.values.length - 1];
  return (
    <fieldset className="space-y-2" data-risk-scale={scale.dimension}>
      <legend className="text-sm font-semibold text-ink">{t.risks.dimensions[scale.dimension]}</legend>
      <Field
        label={t.risks.scaleLabel(scale.dimension)}
        value={scale.label}
        error={fields[`scales[${index}].label`]}
        onChange={(event) => onChange({ ...scale, label: event.target.value })}
      />
      <ol className="grid gap-2 sm:grid-cols-2">
        {scale.values.map((v, i) => (
          <li key={v.value}>
            <Field
              label={`${scale.label || t.risks.dimensions[scale.dimension]} ${v.value}`}
              value={v.name}
              error={fields[`scales[${index}].values[${i}].name`]}
              hint={held.has(v.value) ? t.risks.valueHeld : undefined}
              onChange={(event) => onChange({ ...scale, values: scale.values.map((x) => (x.value === v.value ? { ...x, name: event.target.value } : x)) })}
            />
          </li>
        ))}
      </ol>
      <div className="flex flex-wrap gap-2">
        {scale.values.length < RISK_MAX_SCALE_VALUE && (
          <Button
            variant="ghost"
            size="sm"
            icon={<Icon.Plus />}
            onClick={() =>
              onChange({
                ...scale,
                values: [
                  ...scale.values,
                  { value: scale.values.length + 1, name: t.risks.valueName(scale.dimension, scale.values.length + 1), description: "" },
                ],
              })
            }
          >
            {t.risks.addValue(scale.dimension)}
          </Button>
        )}
        {highest && scale.values.length > 1 && (
          <Button variant="ghost" size="sm" disabled={held.has(highest.value)} onClick={() => onChange({ ...scale, values: scale.values.slice(0, -1) })}>
            {t.risks.removeValue(scale.dimension)}
          </Button>
        )}
      </div>
    </fieldset>
  );
}

/** The levels, least severe first, each with its colour, whether it is acceptable and, by priority number, where it
 * starts. */
function LevelFields({ draft, fields, onChange }: { draft: Draft; fields: Record<string, string>; onChange: (next: Draft) => void }) {
  function setLevel(index: number, next: Level) {
    // The matrix names its cells' levels, so a renamed level takes its cells along.
    const before = draft.levels[index]!.name;
    const matrix = draft.matrix.map((c) => (c.level === before ? { ...c, level: next.name } : c));
    onChange({ ...draft, levels: draft.levels.map((l, i) => (i === index ? next : l)), matrix });
  }
  return (
    <fieldset className="space-y-2" data-risk-levels="">
      <legend className="text-sm font-semibold text-ink">{t.risks.levels}</legend>
      <p className="text-xs text-ink-muted">{t.risks.levelsHint}</p>
      <ol className="space-y-2">
        {draft.levels.map((level, i) => (
          <li key={i} className="grid items-end gap-2 rounded-control border border-border p-2 sm:grid-cols-[1fr_12rem_auto_auto]" data-risk-level={level.name}>
            <Field
              label={t.risks.levelName(i + 1)}
              value={level.name}
              error={fields[`levels[${i}].name`]}
              onChange={(event) => setLevel(i, { ...level, name: event.target.value })}
            />
            <ColorField label={t.risks.levelColour(i + 1)} value={level.colour} onChange={(colour) => setLevel(i, { ...level, colour })} />
            {draft.method === "rpn" && i > 0 ? (
              <Field
                label={t.risks.minRpn(level.name)}
                type="number"
                min={1}
                value={level.minRpn ?? ""}
                error={fields[`levels[${i}].minRpn`]}
                onChange={(event) => setLevel(i, { ...level, minRpn: event.target.value === "" ? null : Number(event.target.value) })}
              />
            ) : (
              <span />
            )}
            <span className="flex items-center gap-2 pb-2">
              <Checkbox
                label={t.risks.acceptable(level.name)}
                checked={level.acceptable}
                onChange={(event) => setLevel(i, { ...level, acceptable: event.target.checked })}
              />
              {draft.levels.length > 1 && (
                <IconButton
                  icon={<Icon.Trash />}
                  label={t.risks.removeLevel(level.name)}
                  size="sm"
                  onClick={() => onChange({ ...draft, levels: draft.levels.filter((_, at) => at !== i) })}
                />
              )}
            </span>
          </li>
        ))}
      </ol>
      {draft.levels.length < RISK_MAX_LEVELS && (
        <Button
          variant="ghost"
          size="sm"
          icon={<Icon.Plus />}
          onClick={() =>
            onChange({
              ...draft,
              levels: [
                ...draft.levels,
                { name: "", colour: TYPE_COLOURS[draft.levels.length % TYPE_COLOURS.length] ?? TYPE_COLOURS[0], acceptable: false, minRpn: null },
              ],
            })
          }
          data-action="add-risk-level"
        >
          {t.risks.addLevel}
        </Button>
      )}
    </fieldset>
  );
}

/** The matrix: a level for every cell of severity across and probability down. */
function MatrixEditor({ draft, onChange }: { draft: Draft; onChange: (next: Draft) => void }) {
  const severity = scaleOf(draft, "severity");
  const probability = scaleOf(draft, "probability");
  if (!severity || !probability) return null;
  return (
    <section className="space-y-2" aria-labelledby="risk-matrix-title">
      <h3 id="risk-matrix-title" className="text-sm font-semibold text-ink">
        {t.risks.matrix}
      </h3>
      <p className="text-xs text-ink-muted">{t.risks.matrixHint}</p>
      <div className="overflow-x-auto">
        <Table aria-labelledby="risk-matrix-title" dense data-risk-matrix-editor="">
          <thead>
            <tr>
              <Th>
                <span className="sr-only">{probability.label}</span>
              </Th>
              {severity.values.map((v) => (
                <Th key={v.value} title={v.name}>
                  {severity.label} {v.value}
                </Th>
              ))}
            </tr>
          </thead>
          <tbody>
            {[...probability.values].reverse().map((p) => (
              <tr key={p.value}>
                <Th scope="row" title={p.name}>
                  {probability.label} {p.value}
                </Th>
                {severity.values.map((s) => {
                  const cell = draft.matrix.find((c) => c.severity === s.value && c.probability === p.value);
                  const colour = draft.levels.find((l) => l.name === cell?.level)?.colour;
                  return (
                    <Td key={s.value}>
                      <span className="flex items-center gap-1.5">
                        <span aria-hidden="true" className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: colour }} />
                        <SelectInput
                          aria-label={t.risks.cell(s.value, p.value)}
                          controlSize="sm"
                          value={cell?.level ?? ""}
                          onChange={(event) =>
                            onChange({
                              ...draft,
                              matrix: draft.matrix.map((c) => (c === cell ? { ...c, level: event.target.value } : c)),
                            })
                          }
                        >
                          {draft.levels.map((l, i) => (
                            <option key={i} value={l.name}>
                              {l.name}
                            </option>
                          ))}
                        </SelectInput>
                      </span>
                    </Td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </Table>
      </div>
    </section>
  );
}
