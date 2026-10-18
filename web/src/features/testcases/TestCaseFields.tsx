import { Button, Field, IconButton, Labelled } from "@/components/ui";
import { Icon } from "@/components/icons";
import { findPeople } from "@/api/requirements";
import { Editor } from "@/features/editor/Editor";
import { t } from "@/i18n";
import { draftKey, type ParameterDraft, type StepDraft, type TestCaseDraft } from "./draft";

type Upload = Parameters<typeof Editor>[0]["upload"];

/** What a test case carries, written beside its statement: preconditions, steps in order, and parameters. */
export function TestCaseFields({
  value,
  onChange,
  errors,
  upload,
  readOnly = false,
}: {
  value: TestCaseDraft;
  onChange: (next: TestCaseDraft) => void;
  /** The server's refusals by field, such as steps[2].action. */
  errors: Record<string, string>;
  upload?: Upload;
  readOnly?: boolean;
}) {
  const setSteps = (steps: StepDraft[]) => onChange({ ...value, steps });
  const setParameters = (parameters: ParameterDraft[]) => onChange({ ...value, parameters });
  return (
    <div className="space-y-4" data-test-case-fields="">
      <Labelled id="edit-preconditions" label={t.testcases.preconditions} hint={t.testcases.preconditionsHint} error={errors.preconditions}>
        <Editor
          id="edit-preconditions"
          value={value.preconditions}
          onChange={(preconditions) => onChange({ ...value, preconditions })}
          mentions={findPeople}
          upload={upload}
          aria-label={t.testcases.preconditions}
        />
      </Labelled>
      <StepList steps={value.steps} onChange={setSteps} errors={errors} upload={upload} readOnly={readOnly} />
      <ParameterList parameters={value.parameters} onChange={setParameters} errors={errors} />
    </div>
  );
}

function StepList({
  steps,
  onChange,
  errors,
  upload,
  readOnly,
}: {
  steps: StepDraft[];
  onChange: (steps: StepDraft[]) => void;
  errors: Record<string, string>;
  upload?: Upload;
  readOnly: boolean;
}) {
  const move = (from: number, to: number) => {
    const next = [...steps];
    const [step] = next.splice(from, 1);
    if (step) next.splice(to, 0, step);
    onChange(next);
  };
  const change = (i: number, patch: Partial<StepDraft>) => onChange(steps.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  return (
    <fieldset className="space-y-3" data-steps="">
      <legend className="text-sm font-medium text-ink">{t.testcases.steps}</legend>
      {errors.steps && (
        <p role="alert" className="text-sm text-danger">
          {errors.steps}
        </p>
      )}
      {steps.length === 0 && <p className="text-sm text-ink-muted">{t.testcases.noSteps}</p>}
      <ol className="space-y-3">
        {steps.map((step, i) => {
          const n = i + 1;
          return (
            <li key={step.key} className="space-y-2 rounded-control border border-border p-3" data-step={n}>
              <div className="flex items-center gap-1">
                <span className="flex-1 text-sm font-semibold text-ink">{t.testcases.step(n)}</span>
                {!readOnly && (
                  <>
                    <IconButton size="sm" icon={<Icon.ChevronUp />} label={t.testcases.moveUp(n)} disabled={i === 0} onClick={() => move(i, i - 1)} />
                    <IconButton
                      size="sm"
                      icon={<Icon.ChevronDown />}
                      label={t.testcases.moveDown(n)}
                      disabled={i === steps.length - 1}
                      onClick={() => move(i, i + 1)}
                    />
                    <IconButton size="sm" icon={<Icon.X />} label={t.testcases.remove(n)} onClick={() => onChange(steps.filter((_, j) => j !== i))} />
                  </>
                )}
              </div>
              <Labelled id={`step-${step.key}-action`} label={t.testcases.action} error={errors[`steps[${i}].action`] ?? errors[`steps[${i}].actionMarkdown`]}>
                <Editor
                  id={`step-${step.key}-action`}
                  value={step.action}
                  onChange={(action) => change(i, { action })}
                  mentions={findPeople}
                  upload={upload}
                  aria-label={`${t.testcases.step(n)}: ${t.testcases.action}`}
                />
              </Labelled>
              <Labelled id={`step-${step.key}-expected`} label={t.testcases.expected} error={errors[`steps[${i}].expected`]}>
                <Editor
                  id={`step-${step.key}-expected`}
                  value={step.expected}
                  onChange={(expected) => change(i, { expected })}
                  mentions={findPeople}
                  upload={upload}
                  aria-label={`${t.testcases.step(n)}: ${t.testcases.expected}`}
                />
              </Labelled>
            </li>
          );
        })}
      </ol>
      {!readOnly && (
        <Button
          type="button"
          size="sm"
          variant="secondary"
          icon={<Icon.Plus />}
          onClick={() => onChange([...steps, { key: draftKey(), action: null, expected: null }])}
          data-action="add-step"
        >
          {t.testcases.addStep}
        </Button>
      )}
    </fieldset>
  );
}

function ParameterList({
  parameters,
  onChange,
  errors,
}: {
  parameters: ParameterDraft[];
  onChange: (parameters: ParameterDraft[]) => void;
  errors: Record<string, string>;
}) {
  const change = (i: number, patch: Partial<ParameterDraft>) => onChange(parameters.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  return (
    <fieldset className="space-y-2" aria-describedby="parameters-hint" data-parameters="">
      <legend className="text-sm font-medium text-ink">{t.testcases.parameters}</legend>
      <p id="parameters-hint" className="text-xs text-ink-muted">
        {t.testcases.parametersHint}
      </p>
      {errors.parameters && (
        <p role="alert" className="text-sm text-danger">
          {errors.parameters}
        </p>
      )}
      {parameters.map((p, i) => (
        <div key={p.key} className="grid items-end gap-2 sm:grid-cols-[12rem_1fr_auto]" data-parameter={i + 1}>
          <Field
            label={t.testcases.parameterName}
            id={`parameter-${p.key}-name`}
            value={p.name}
            onChange={(event) => change(i, { name: event.target.value })}
            error={errors[`parameters[${i}].name`]}
            controlSize="sm"
          />
          <Field
            label={t.testcases.parameterValues}
            id={`parameter-${p.key}-values`}
            value={p.values}
            onChange={(event) => change(i, { values: event.target.value })}
            error={errors[`parameters[${i}].values`]}
            controlSize="sm"
          />
          <IconButton size="sm" icon={<Icon.X />} label={t.testcases.removeParameter(p.name)} onClick={() => onChange(parameters.filter((_, j) => j !== i))} />
        </div>
      ))}
      <Button
        type="button"
        size="sm"
        variant="secondary"
        icon={<Icon.Plus />}
        onClick={() => onChange([...parameters, { key: draftKey(), name: "", values: "" }])}
        data-action="add-parameter"
      >
        {t.testcases.addParameter}
      </Button>
    </fieldset>
  );
}
