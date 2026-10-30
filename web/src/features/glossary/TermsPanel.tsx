import { useId, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useClearTermUse, useRequirementTerms, type RequirementTerm } from "@/api/glossary";
import type { Project } from "@/api/projects";
import type { Requirement } from "@/api/requirements";
import { Button, Card, Dialog, ErrorBanner, Field, SectionTitle, Skeleton, Tag, useToast, DialogFooter } from "@/components/ui";
import { TERM_COMMENT_MAX_LENGTH, TERM_COMMENT_ROWS } from "@/config";
import { t } from "@/i18n";

/** The glossary terms a requirement uses, by its words or linked on purpose (#210), each leading to its term; a use
 * written against an older definition is suspect, and the requirement's editor confirms it still holds. */
export function TermsPanel({ project, requirement }: { project: Project; requirement: Requirement }) {
  const section = requirement.kind === "section";
  const { data: terms, error, refetch } = useRequirementTerms(requirement.key, !section);
  const [confirming, setConfirming] = useState<RequirementTerm | null>(null);
  const toast = useToast();
  if (section) return null;
  const l = t.glossary.panel;
  const may = requirement.can.edit && !project.archivedAt && !requirement.trashedAt;
  return (
    <Card className="space-y-3 p-4" data-terms-panel>
      <section aria-labelledby="terms-panel-title" className="space-y-3">
        <SectionTitle id="terms-panel-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!terms && !error && <Skeleton />}
        {terms && terms.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {terms && terms.length > 0 && (
          <ul className="space-y-1.5">
            {terms.map((use) => (
              <li key={use.term.id} className="flex flex-wrap items-center gap-2 text-sm" data-requirement-term={use.term.name} data-suspect={use.suspect}>
                <Link
                  to="/projects/$projectKey/glossary/$termId"
                  params={{ projectKey: use.term.projectKey, termId: use.term.id }}
                  className="font-medium text-ink hover:text-accent hover:underline"
                >
                  {use.term.name}
                </Link>
                {use.term.projectKey !== project.key && <Tag>{use.term.projectKey}</Tag>}
                <Tag>{use.explicit ? l.explicit : l.byWords}</Tag>
                {use.term.status === "deprecated" && <Tag>{t.glossary.statuses.deprecated}</Tag>}
                {use.suspect && (
                  <Tag className="border-warning bg-warning-subtle text-ink" title={l.suspectHint(use.seenRevision, use.definitionRevision)} data-term-suspect>
                    {l.suspect}
                  </Tag>
                )}
                {use.suspect && <span className="sr-only">{l.suspectHint(use.seenRevision, use.definitionRevision)}</span>}
                {use.suspect && may && (
                  <Button
                    variant="secondary"
                    size="sm"
                    aria-label={l.confirmLabel(use.term.name)}
                    onClick={() => {
                      setConfirming(use);
                    }}
                    data-action="confirm-term"
                  >
                    {l.confirm}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      {confirming && (
        <ConfirmDialog
          reqKey={requirement.key}
          use={confirming}
          onClose={() => setConfirming(null)}
          onDone={() => {
            toast.success(l.confirmed(confirming.term.name));
            setConfirming(null);
          }}
        />
      )}
    </Card>
  );
}

function ConfirmDialog({ reqKey, use, onClose, onDone }: { reqKey: string; use: RequirementTerm; onClose: () => void; onDone: () => void }) {
  const formId = useId();
  const l = t.glossary.panel;
  const [comment, setComment] = useState("");
  const clear = useClearTermUse(reqKey);
  const fields = clear.error instanceof ApiError ? clear.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    // The use is confirmed under the definition it now has, the revision that made it suspect.
    clear.mutate({ termId: use.term.id, termRevision: use.definitionRevision, comment: comment.trim() }, { onSuccess: onDone });
  }
  return (
    <Dialog open title={l.confirmTitle} onClose={onClose} attrs={{ "data-confirm-term": "" }}>
      <form id={formId} onSubmit={submit} className="space-y-4" noValidate>
        <p className="text-sm text-ink">{l.confirmIntro(use.term.name, use.definitionRevision)}</p>
        {clear.error && !fields.comment && <ErrorBanner>{clear.error.message}</ErrorBanner>}
        <Field
          label={l.comment}
          hint={l.commentHint}
          value={comment}
          rows={TERM_COMMENT_ROWS}
          maxLength={TERM_COMMENT_MAX_LENGTH}
          error={fields.comment}
          onChange={(event) => setComment(event.target.value)}
          autoFocus
        />
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" form={formId} loading={clear.isPending} disabled={!comment.trim()} data-action="send-confirm-term">
            {l.confirmSend}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}
