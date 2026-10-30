import { useState, type FormEvent } from "react";
import { useTypes } from "@/api/attributes";
import { ApiError } from "@/api/client";
import { linkWays, useProjectLinkTypes } from "@/api/links";
import type { Project } from "@/api/projects";
import { LEVEL_KINDS, useSaveTraceModel, useTraceModel, type LevelKind, type TraceModel, type TraceModelInput, type TraceModelStep } from "@/api/trace";
import { Button, Card, ErrorBanner, Field, IconButton, SectionTitle, Select, Skeleton, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TRACE_LEVEL_NAME_MAX, TRACE_MAX_LEVELS } from "@/config";
import { t } from "@/i18n";
import { CODE_RELATIONS, ISSUE_RELATIONS, PAGE_RELATIONS, TEST_RELATIONS } from "./address";
import { Choices } from "./Choices";

/** A level as the card edits it; a step is `typeId:direction` for a link type, or a relation. */
interface Draft {
  name: string;
  kind: LevelKind;
  typeIds: string[];
  steps: string[];
}

function drafts(model: TraceModel): Draft[] {
  return model.levels.map((level) => ({
    name: level.name,
    kind: level.kind,
    typeIds: level.types.map((type) => type.id),
    steps: level.steps.map((step) => (step.typeId ? `${step.typeId}:${step.direction}` : (step.relation ?? ""))),
  }));
}

function input(levels: Draft[]): TraceModelInput {
  return {
    levels: levels.map((level, i) => ({
      name: level.name,
      kind: level.kind,
      typeIds: level.kind === "requirements" ? level.typeIds : [],
      steps:
        i === 0
          ? []
          : level.steps.map((step): TraceModelStep => {
              const [typeId, direction] = step.split(":");
              return level.kind === "requirements"
                ? { typeId, direction: direction as TraceModelStep["direction"] }
                : { relation: step as TraceModelStep["relation"] };
            }),
    })),
  };
}

const capitalized = (phrase: string) => phrase.charAt(0).toUpperCase() + phrase.slice(1);

/** A project's trace model (#151), its levels added, reordered and removed in place by its administrators. */
export function TraceModelCard({ project }: { project: Project }) {
  const { data: model, error, refetch } = useTraceModel(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!model) return <Skeleton />;
  return <ModelForm key={model.updatedAt ?? "unset"} project={project} model={model} />;
}

function ModelForm({ project, model }: { project: Project; model: TraceModel }) {
  const toast = useToast();
  const [levels, setLevels] = useState<Draft[]>(() => drafts(model));
  const save = useSaveTraceModel(project.key);
  const { data: types = [] } = useTypes(project.key);
  const { data: linkTypes = [] } = useProjectLinkTypes(project.key);
  const may = model.can.edit;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const ways = linkWays(linkTypes, capitalized);
  const set = (i: number, patch: Partial<Draft>) => setLevels(levels.map((level, j) => (j === i ? { ...level, ...patch } : level)));
  const move = (i: number, by: number) => {
    const next = [...levels];
    [next[i], next[i + by]] = [next[i + by]!, next[i]!];
    setLevels(next);
  };
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate(input(levels), { onSuccess: () => toast.success(t.chains.saved) });
  }
  return (
    <Card className="space-y-4 p-4" data-trace-model>
      <SectionTitle>{t.chains.modelTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.chains.modelIntro}</p>
      {!may && <p className="text-sm text-ink-muted">{t.chains.readOnly}</p>}
      {levels.length === 0 && <p className="text-sm text-ink-subtle">{t.chains.noModel}</p>}
      <form onSubmit={submit} className="space-y-3" aria-label={t.chains.modelTitle}>
        <ol className="space-y-3">
          {levels.map((level, i) => {
            const label = level.name || t.chains.level(i + 1);
            const errors = Object.entries(fields).filter(([field]) => field.startsWith(`levels[${i}]`));
            return (
              <li key={i} className="space-y-2 rounded-control border border-border p-3" data-trace-level={i}>
                <div className="flex flex-wrap items-end gap-3">
                  <div className="min-w-48 flex-1">
                    <Field
                      id={`trace-level-${i}-name`}
                      label={t.chains.name}
                      value={level.name}
                      maxLength={TRACE_LEVEL_NAME_MAX}
                      disabled={!may}
                      onChange={(event) => set(i, { name: event.target.value })}
                    />
                  </div>
                  <div className="min-w-44">
                    <Select
                      id={`trace-level-${i}-kind`}
                      label={t.chains.kind}
                      value={level.kind}
                      disabled={!may || i === 0}
                      onChange={(event) => set(i, { kind: event.target.value as LevelKind, steps: [] })}
                    >
                      {LEVEL_KINDS.map((kind) => (
                        <option key={kind} value={kind}>
                          {t.chains.kinds[kind]}
                        </option>
                      ))}
                    </Select>
                  </div>
                  {may && (
                    <div className="flex gap-1">
                      <IconButton icon={<Icon.ChevronUp />} label={t.chains.moveUp(label)} size="sm" disabled={i === 0} onClick={() => move(i, -1)} />
                      <IconButton
                        icon={<Icon.ChevronDown />}
                        label={t.chains.moveDown(label)}
                        size="sm"
                        disabled={i === levels.length - 1}
                        onClick={() => move(i, 1)}
                      />
                      <IconButton icon={<Icon.Trash />} label={t.chains.remove(label)} size="sm" onClick={() => setLevels(levels.filter((_, j) => j !== i))} />
                    </div>
                  )}
                </div>
                {level.kind === "requirements" && (
                  <Choices
                    label={t.chains.types}
                    groupLabel={`${t.chains.types}: ${label}`}
                    hint=""
                    quiet
                    name={`level-${i}-type`}
                    options={types.filter((type) => !type.retired).map((type) => ({ value: type.id, label: type.name }))}
                    chosen={level.typeIds}
                    onChange={(typeIds) => may && set(i, { typeIds })}
                  />
                )}
                {i > 0 && (
                  <Choices
                    label={t.chains.steps}
                    groupLabel={`${t.chains.steps}: ${label}`}
                    hint=""
                    quiet
                    name={`level-${i}-step`}
                    options={
                      level.kind === "requirements"
                        ? ways
                        : level.kind === "issues"
                          ? ISSUE_RELATIONS.map((r) => ({ value: r, label: t.matrix.relationNames[r] }))
                          : level.kind === "tests"
                            ? TEST_RELATIONS.map((r) => ({ value: r, label: t.matrix.relationNames[r] }))
                            : level.kind === "code"
                              ? CODE_RELATIONS.map((r) => ({ value: r, label: t.matrix.relationNames[r] }))
                              : PAGE_RELATIONS.map((r) => ({ value: r, label: t.stator.links.headings[r] }))
                    }
                    chosen={level.steps}
                    onChange={(steps) => may && set(i, { steps })}
                  />
                )}
                {errors.map(([field, message]) => (
                  <p key={field} className="text-sm text-danger" data-trace-level-error={field}>
                    {message}
                  </p>
                ))}
              </li>
            );
          })}
        </ol>
        {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
        {may && (
          <div className="flex flex-wrap gap-2">
            <Button
              variant="secondary"
              icon={<Icon.Plus />}
              disabled={levels.length >= TRACE_MAX_LEVELS}
              onClick={() => setLevels([...levels, { name: "", kind: "requirements", typeIds: [], steps: [] }])}
              data-action="add-trace-level"
            >
              {t.chains.addLevel}
            </Button>
            <Button type="submit" loading={save.isPending} data-action="save-trace-model">
              {t.chains.save}
            </Button>
          </div>
        )}
      </form>
    </Card>
  );
}
