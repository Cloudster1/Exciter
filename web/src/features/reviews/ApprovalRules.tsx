import { useState, type FormEvent } from "react";
import { useTypes } from "@/api/attributes";
import { ApiError } from "@/api/client";
import { subjectKey, type Subject } from "@/api/permissions";
import type { Project } from "@/api/projects";
import { approverSubject, subjectApprover, useApprovalRules, useDeleteApprovalRule, useSaveApprovalRule, type ApprovalRule } from "@/api/reviews";
import { Button, Card, Checkbox, ErrorBanner, Field, IconButton, SectionTitle, Select, Skeleton, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REVIEW_REQUIRED_MAX } from "@/config";
import { SubjectGlyph, SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";

// A project's approval rules (#35), after Stator's ReviewRuleForm, one rule per requirement type.

/** Who approves a project's requirements, a rule per type and one for every other type. */
export function ApprovalRules({ project }: { project: Project }) {
  const { data, error, isLoading, refetch } = useApprovalRules(project.key);
  const { data: types } = useTypes(project.key);
  const [editing, setEditing] = useState<ApprovalRule | "new" | null>(null);
  const toast = useToast();
  const confirm = useConfirm();
  const remove = useDeleteApprovalRule(project.key);
  const mayChange = project.can.administer && !project.archivedAt;
  const rules = data ?? [];
  return (
    <Card className="space-y-4 p-4" data-approval-rules="">
      <SectionTitle>{t.review.rulesTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.review.rulesIntro}</p>
      {!mayChange && <p className="text-sm text-ink-muted">{t.review.readOnly}</p>}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading ? (
        <Skeleton />
      ) : rules.length === 0 ? (
        <p className="text-sm text-ink-muted" data-approval-rules-empty="">
          {t.review.rulesEmpty}
        </p>
      ) : (
        <ul className="divide-y divide-border" aria-label={t.review.rulesTitle}>
          {rules.map((rule) => {
            const label = rule.typeId ? rule.typeName : t.review.everyType;
            return (
              <li key={rule.id} className="flex flex-wrap items-start gap-2 py-2" data-approval-rule={label}>
                <div className="min-w-0 flex-1 text-sm">
                  <p className="font-medium text-ink">{label}</p>
                  <p className="text-ink-muted">{t.review.ruleSummary(rule.required, rule.approvers.map((a) => a.name).join(", "))}</p>
                  {rule.authorMayApprove && <p className="text-xs text-ink-subtle">{t.review.authorAllowed}</p>}
                </div>
                {mayChange && (
                  <div className="flex gap-1">
                    <Button size="sm" variant="ghost" onClick={() => setEditing(rule)} aria-label={`${t.review.editRule} ${label}`} data-action="edit-rule">
                      {t.review.editRule}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      loading={remove.isPending && remove.variables === rule.id}
                      aria-label={`${t.review.remove} ${label}`}
                      onClick={async () => {
                        if (!(await confirm({ noun: t.kit.confirm.things.thisRule, verb: t.kit.confirm.verbs.remove, body: t.review.confirmRemove }))) return;
                        remove.mutate(rule.id, { onSuccess: () => toast.success(t.review.removed) });
                      }}
                      data-action="remove-rule"
                    >
                      {t.review.remove}
                    </Button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {mayChange &&
        (editing ? (
          <RuleForm
            key={editing === "new" ? "new" : editing.id}
            project={project}
            rule={editing === "new" ? null : editing}
            types={(types ?? []).map((type) => ({ id: type.id, name: type.name }))}
            onDone={(saved) => {
              setEditing(null);
              if (saved) toast.success(t.review.saved);
            }}
          />
        ) : (
          <Button
            variant="secondary"
            icon={<Icon.Plus />}
            onClick={() => {
              setEditing("new");
            }}
            data-action="new-rule"
          >
            {t.review.newRule}
          </Button>
        ))}
    </Card>
  );
}

function RuleForm({
  project,
  rule,
  types,
  onDone,
}: {
  project: Project;
  rule: ApprovalRule | null;
  types: { id: string; name: string }[];
  onDone: (saved: boolean) => void;
}) {
  const save = useSaveApprovalRule(project.key);
  const [typeId, setTypeId] = useState(rule?.typeId ?? "");
  const [approvers, setApprovers] = useState<Subject[]>(rule?.approvers.map(approverSubject) ?? []);
  const [required, setRequired] = useState(String(rule?.required ?? 1));
  const [authorMayApprove, setAuthorMayApprove] = useState(rule?.authorMayApprove ?? false);
  const [problems, setProblems] = useState<{ approvers?: string; required?: string }>({});
  const fields = save.error instanceof ApiError ? save.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    save.reset();
    const count = Number(required);
    const found = {
      approvers: approvers.length === 0 ? t.review.approversNeeded : undefined,
      required: Number.isInteger(count) && count >= 1 && count <= REVIEW_REQUIRED_MAX ? undefined : t.review.requiredRange(REVIEW_REQUIRED_MAX),
    };
    setProblems(found);
    if (found.approvers || found.required) return;
    save.mutate(
      { typeId: typeId || undefined, required: count, authorMayApprove, approvers: approvers.map(subjectApprover) },
      { onSuccess: () => onDone(true) },
    );
  }

  const approversError = problems.approvers ?? fields.approvers;
  return (
    <form onSubmit={submit} className="space-y-3 rounded-overlay border border-border p-3" noValidate data-rule-form="">
      {save.error && !fields.approvers && !fields.required && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <Select label={t.review.ruleFor} value={typeId} disabled={rule !== null} onChange={(event) => setTypeId(event.target.value)} data-field="rule-type">
        <option value="">{t.review.everyType}</option>
        {types.map((type) => (
          <option key={type.id} value={type.id}>
            {type.name}
          </option>
        ))}
      </Select>
      <fieldset className="space-y-2" aria-describedby={approversError ? "rule-approvers-error" : undefined}>
        <legend className="mb-1 text-sm font-medium text-ink">{t.review.approversTitle}</legend>
        {approvers.length === 0 ? (
          <p className="text-sm text-ink-muted">{t.review.approversEmpty}</p>
        ) : (
          <ul className="space-y-1" aria-label={t.review.approversTitle}>
            {approvers.map((a) => (
              <li key={subjectKey(a)} className="flex items-center gap-2 text-sm" data-approver={a.name}>
                <SubjectGlyph type={a.type} />
                <span className="min-w-0 flex-1 truncate">{a.name}</span>
                <IconButton
                  icon={<Icon.X />}
                  label={t.review.removeApprover(a.name)}
                  onClick={() => setApprovers(approvers.filter((each) => subjectKey(each) !== subjectKey(a)))}
                />
              </li>
            ))}
          </ul>
        )}
        <SubjectPicker
          label={t.review.pickApprover}
          inline
          exclude={approvers.map(subjectKey)}
          onPick={(picked) => {
            setProblems((before) => ({ ...before, approvers: undefined }));
            setApprovers([...approvers, picked]);
          }}
        />
        {approversError && (
          <p id="rule-approvers-error" className="text-sm text-danger">
            {approversError}
          </p>
        )}
      </fieldset>
      <Field
        type="number"
        label={t.review.required}
        hint={t.review.requiredHint(REVIEW_REQUIRED_MAX)}
        min={1}
        max={REVIEW_REQUIRED_MAX}
        value={required}
        error={problems.required ?? fields.required}
        onChange={(event) => setRequired(event.target.value)}
        className="w-24"
      />
      <Checkbox label={t.review.authorMayApprove} checked={authorMayApprove} onChange={(event) => setAuthorMayApprove(event.target.checked)} />
      <div className="flex gap-2">
        <Button type="submit" loading={save.isPending} data-action="save-rule">
          {t.review.save}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onDone(false)}>
          {t.review.cancel}
        </Button>
      </div>
    </form>
  );
}
