import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useBaselines } from "@/api/baselines";
import { ApiError } from "@/api/client";
import { subjectKey, type Subject } from "@/api/permissions";
import type { Project } from "@/api/projects";
import { useExecutionActions, type TestPlan } from "@/api/tests";
import { Icon } from "@/components/icons";
import { Button, Dialog, ErrorBanner, Field, IconButton, Select } from "@/components/ui";
import { TEST_CONFIGURATION_MAX_LENGTH, TEST_TITLE_MAX_LENGTH, TEST_VERSION_MAX_LENGTH } from "@/config";
import { SubjectGlyph, SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";

/** Starts an execution of a plan: its target, due date and testers; its cases are pinned as it starts. */
export function StartExecutionDialog({ project, plan, onClose }: { project: Project; plan: TestPlan; onClose: () => void }) {
  const { start } = useExecutionActions(project.key);
  const { data: baselines } = useBaselines(project.key);
  const navigate = useNavigate();
  const [title, setTitle] = useState(plan.title);
  const [baselineId, setBaselineId] = useState("");
  const [version, setVersion] = useState("");
  const [configuration, setConfiguration] = useState("");
  const [due, setDue] = useState("");
  const [testers, setTesters] = useState<Subject[]>([]);
  const [problem, setProblem] = useState("");
  const fields = start.error instanceof ApiError ? start.error.fields : {};
  const general = start.error && Object.keys(fields).length === 0;
  const testersError = problem || fields.testers;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (testers.length === 0) {
      setProblem(t.tests.noTesters);
      return;
    }
    setProblem("");
    start.mutate(
      {
        title: title.trim(),
        planId: plan.id,
        ...(baselineId ? { baselineId } : {}),
        version: version.trim() || undefined,
        configuration: configuration.trim() || undefined,
        dueAt: due ? new Date(due).toISOString() : undefined,
        testers: testers.map((s) =>
          s.type === "user" ? { subjectType: "user", userId: s.id ?? undefined } : { subjectType: "group", groupId: s.id ?? undefined },
        ),
      },
      {
        onSuccess: (e) => void navigate({ to: "/p/$projectKey/tests/executions/$executionId", params: { projectKey: project.key, executionId: e.id } }),
      },
    );
  }

  return (
    <Dialog title={t.tests.startTitle} onClose={onClose} wide>
      <form onSubmit={submit} className="space-y-3" noValidate data-start-execution>
        {general && <ErrorBanner>{start.error?.message}</ErrorBanner>}
        <Field label={t.tests.executionTitle} value={title} maxLength={TEST_TITLE_MAX_LENGTH} error={fields.title} onChange={(e) => setTitle(e.target.value)} />
        {fields.planId && <ErrorBanner>{fields.planId}</ErrorBanner>}
        <Select label={t.tests.baseline} value={baselineId} error={fields.baselineId} onChange={(e) => setBaselineId(e.target.value)} data-execution-baseline>
          <option value="">{t.tests.noBaseline}</option>
          {(baselines ?? []).map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </Select>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field
            label={t.tests.version}
            value={version}
            maxLength={TEST_VERSION_MAX_LENGTH}
            error={fields.version}
            onChange={(e) => setVersion(e.target.value)}
          />
          <Field
            label={t.tests.configuration}
            value={configuration}
            maxLength={TEST_CONFIGURATION_MAX_LENGTH}
            error={fields.configuration}
            onChange={(e) => setConfiguration(e.target.value)}
          />
        </div>
        <Field type="datetime-local" label={t.tests.due} value={due} error={fields.dueAt} onChange={(e) => setDue(e.target.value)} />
        <fieldset className="space-y-2" aria-describedby={testersError ? "execution-testers-hint execution-testers-error" : "execution-testers-hint"}>
          <legend className="text-sm font-medium text-ink">{t.tests.testers}</legend>
          <p id="execution-testers-hint" className="text-xs text-ink-muted">
            {t.tests.testersHint}
          </p>
          {testers.length > 0 && (
            <ul className="space-y-1">
              {testers.map((s) => (
                <li key={subjectKey(s)} className="flex items-center gap-2 text-sm" data-tester={s.name}>
                  <SubjectGlyph type={s.type} />
                  <span className="flex-1">{s.name}</span>
                  <IconButton
                    icon={<Icon.X />}
                    label={t.tests.removeTester(s.name)}
                    onClick={() => setTesters(testers.filter((each) => subjectKey(each) !== subjectKey(s)))}
                  />
                </li>
              ))}
            </ul>
          )}
          <SubjectPicker
            label={t.tests.addTester}
            inline
            exclude={testers.map(subjectKey)}
            onPick={(picked) => {
              setProblem("");
              setTesters([...testers, picked]);
            }}
          />
          {testersError && (
            <p id="execution-testers-error" className="text-sm text-danger">
              {testersError}
            </p>
          )}
        </fieldset>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.tests.cancel}
          </Button>
          <Button type="submit" loading={start.isPending} disabled={!title.trim()} data-action="start-execution-send">
            {t.tests.startSend}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
