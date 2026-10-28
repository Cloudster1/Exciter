import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useFieldRules, useSaveFieldRule, type FieldPermit, type FieldRule } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { useStatuses, type Status } from "@/api/workflows";
import { Button, Card, ErrorBanner, SectionTitle, Skeleton } from "@/components/ui";
import { t } from "@/i18n";
import { PermitsEditor, RequiredFrom } from "./FieldRuleControls";

/** When a requirement's own fields are required and who changes them (#206), a form per field. */
export function FixedFieldRules({ project }: { project: Project }) {
  const { data: rules, error, refetch } = useFieldRules(project.key);
  const { data: statuses } = useStatuses(project.key);
  const mayChange = project.can.administer && !project.archivedAt;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!rules) return <Skeleton />;
  return (
    <Card className="space-y-4 p-4" data-fixed-field-rules>
      <SectionTitle>{t.fieldRules.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.fieldRules.intro}</p>
      {!mayChange && <p className="text-sm text-ink-muted">{t.fieldRules.readOnlySettings}</p>}
      <div className="divide-y divide-border">
        {rules.map((rule) => (
          // Keyed by the field alone: a key following the saved rule remounted the form and dropped its notice.
          <RuleForm key={rule.field} project={project} rule={rule} statuses={statuses ?? []} mayChange={mayChange} />
        ))}
      </div>
    </Card>
  );
}

function RuleForm({ project, rule, statuses, mayChange }: { project: Project; rule: FieldRule; statuses: Status[]; mayChange: boolean }) {
  const save = useSaveFieldRule(project.key);
  const [requiredFrom, setRequiredFrom] = useState<string | null>(rule.requiredFrom);
  const [permits, setPermits] = useState<FieldPermit[]>(rule.editableBy);
  const [notice, setNotice] = useState("");
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const always = rule.field === "title" || rule.field === "statement";
  const name = t.fieldRules.names[rule.field];

  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    save.mutate(
      { field: rule.field, requiredFrom: always ? null : requiredFrom, editableBy: permits },
      { onSuccess: () => setNotice(t.fieldRules.saved(name)) },
    );
  }

  return (
    <form onSubmit={submit} className="space-y-3 py-3" aria-label={name} data-fixed-field={rule.field}>
      <h3 className="text-sm font-semibold text-ink">{name}</h3>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <fieldset disabled={!mayChange} className="space-y-3">
        {always ? (
          <p className="text-sm text-ink-muted">{t.fieldRules.always}</p>
        ) : (
          <RequiredFrom
            id={`fixed-${rule.field}-required-from`}
            statuses={statuses}
            value={requiredFrom}
            onChange={setRequiredFrom}
            error={fields.requiredFrom}
          />
        )}
        <PermitsEditor id={`fixed-${rule.field}-permits`} statuses={statuses} permits={permits} onChange={setPermits} error={fields.editableBy} />
        {mayChange && (
          <Button type="submit" size="sm" loading={save.isPending} data-action="save-field-rule">
            {t.fieldRules.save}
          </Button>
        )}
      </fieldset>
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
    </form>
  );
}
