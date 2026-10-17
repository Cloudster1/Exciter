import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useSigningRule, useSigningRuleActions, type BaselineSigner, type SigningRule as Rule } from "@/api/baselines";
import { subjectKey, type Subject } from "@/api/permissions";
import type { Project } from "@/api/projects";
import { Button, Card, ErrorBanner, Field, IconButton, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BASELINE_SIGNATURES_MAX } from "@/config";
import { SubjectGlyph, SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";

// Who signs a project's baselines (#205), shaped as an approval rule is.

function signerSubject(s: BaselineSigner): Subject {
  return s.subjectType === "user" ? { type: "user", id: s.userId ?? null, name: s.name ?? "" } : { type: "group", id: s.groupId ?? null, name: s.name ?? "" };
}

function subjectSigner(s: Subject): BaselineSigner {
  return s.type === "user"
    ? { subjectType: "user", userId: s.id ?? undefined, name: s.name }
    : { subjectType: "group", groupId: s.id ?? undefined, name: s.name };
}

/** The project's signing rule, which its administrators set or remove. */
export function SigningRuleCard({ project }: { project: Project }) {
  const { data: rule, error, isLoading, refetch } = useSigningRule(project.key);
  const { remove } = useSigningRuleActions(project.key);
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState("");
  const mayChange = project.can.administer && !project.archivedAt;
  return (
    <Card className="space-y-4 p-4" data-signing-rule="">
      <SectionTitle>{t.control.ruleTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.control.ruleIntro}</p>
      {!mayChange && <p className="text-sm text-ink-muted">{t.control.readOnly}</p>}
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading ? (
        <Skeleton />
      ) : editing ? (
        <RuleForm
          project={project}
          rule={rule ?? null}
          onDone={(saved) => {
            setEditing(false);
            if (saved) setNotice(t.control.saved);
          }}
        />
      ) : (
        <>
          <p className="text-sm text-ink" data-signing-rule-summary="">
            {rule ? t.control.ruleSummary(rule.required, rule.signers.map((each) => each.name).join(", ")) : t.control.ruleNone}
          </p>
          {mayChange && (
            <div className="flex flex-wrap gap-2">
              <Button
                variant="secondary"
                onClick={() => {
                  setNotice("");
                  setEditing(true);
                }}
                data-action="edit-signing-rule"
              >
                {rule ? t.control.editRule : t.control.newRule}
              </Button>
              {rule && (
                <Button
                  variant="ghost"
                  loading={remove.isPending}
                  onClick={() => {
                    if (!window.confirm(t.control.confirmRemove)) return;
                    setNotice("");
                    remove.mutate(undefined, { onSuccess: () => setNotice(t.control.removed) });
                  }}
                  data-action="remove-signing-rule"
                >
                  {t.control.removeRule}
                </Button>
              )}
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function RuleForm({ project, rule, onDone }: { project: Project; rule: Rule | null; onDone: (saved: boolean) => void }) {
  const { save } = useSigningRuleActions(project.key);
  const [signers, setSigners] = useState<Subject[]>(rule?.signers.map(signerSubject) ?? []);
  const [required, setRequired] = useState(String(rule?.required ?? 1));
  const [problems, setProblems] = useState<{ signers?: string; required?: string }>({});
  const fields = save.error instanceof ApiError ? save.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    save.reset();
    const count = Number(required);
    const found = {
      signers: signers.length === 0 ? t.control.signersNeeded : undefined,
      required: Number.isInteger(count) && count >= 1 && count <= BASELINE_SIGNATURES_MAX ? undefined : t.control.requiredRange(BASELINE_SIGNATURES_MAX),
    };
    setProblems(found);
    if (found.signers || found.required) return;
    save.mutate({ required: count, signers: signers.map(subjectSigner) }, { onSuccess: () => onDone(true) });
  }

  const signersError = problems.signers ?? fields.signers;
  return (
    <form onSubmit={submit} className="space-y-3 rounded-overlay border border-border p-3" noValidate data-signing-rule-form="">
      {save.error && !fields.signers && !fields.required && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <fieldset className="space-y-2" aria-describedby={signersError ? "signing-signers-error" : undefined}>
        <legend className="mb-1 text-sm font-medium text-ink">{t.control.signersTitle}</legend>
        {signers.length === 0 ? (
          <p className="text-sm text-ink-muted">{t.control.signersEmpty}</p>
        ) : (
          <ul className="space-y-1" aria-label={t.control.signersTitle}>
            {signers.map((s) => (
              <li key={subjectKey(s)} className="flex items-center gap-2 text-sm" data-signer={s.name}>
                <SubjectGlyph type={s.type} />
                <span className="min-w-0 flex-1 truncate">{s.name}</span>
                <IconButton
                  icon={<Icon.X />}
                  label={t.control.removeSigner(s.name)}
                  onClick={() => setSigners(signers.filter((each) => subjectKey(each) !== subjectKey(s)))}
                />
              </li>
            ))}
          </ul>
        )}
        <SubjectPicker
          label={t.control.pickSigner}
          inline
          exclude={signers.map(subjectKey)}
          onPick={(picked) => {
            setProblems((before) => ({ ...before, signers: undefined }));
            setSigners([...signers, picked]);
          }}
        />
        {signersError && (
          <p id="signing-signers-error" className="text-sm text-danger">
            {signersError}
          </p>
        )}
      </fieldset>
      <Field
        type="number"
        label={t.control.required}
        hint={t.control.requiredHint(BASELINE_SIGNATURES_MAX)}
        min={1}
        max={BASELINE_SIGNATURES_MAX}
        value={required}
        error={problems.required ?? fields.required}
        onChange={(event) => setRequired(event.target.value)}
        className="w-24"
      />
      <div className="flex gap-2">
        <Button type="submit" loading={save.isPending} data-action="save-signing-rule">
          {t.control.saveRule}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onDone(false)}>
          {t.control.cancel}
        </Button>
      </div>
    </form>
  );
}
