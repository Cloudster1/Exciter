import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { findPeople, useCreateRequirement, type NewRequirement } from "@/api/requirements";
import { Button, ErrorBanner, Field, Labelled, PageHeader, Segmented } from "@/components/ui";
import { REQUIREMENT_TITLE_MAX_LENGTH, REVISION_COMMENT_MAX_LENGTH } from "@/config";
import { Editor } from "@/features/editor/Editor";
import { isEmptyDoc, type Doc } from "@/features/editor/schema";
import { t } from "@/i18n";

type Kind = NonNullable<NewRequirement["kind"]>;

/** Writes a requirement or a section as revision 1, and opens it. */
export function NewRequirementForm({ project }: { project: Project }) {
  const navigate = useNavigate();
  const create = useCreateRequirement(project.key);
  const [kind, setKind] = useState<Kind>("requirement");
  const [title, setTitle] = useState("");
  const [statement, setStatement] = useState<Doc | null>(null);
  const [rationale, setRationale] = useState<Doc | null>(null);
  const [comment, setComment] = useState("");
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const general = create.error && Object.keys(fields).length === 0;

  function submit(event: FormEvent) {
    event.preventDefault();
    const body: NewRequirement = { kind, title, comment };
    if (statement && !isEmptyDoc(statement)) body.statement = statement;
    if (kind === "requirement" && rationale && !isEmptyDoc(rationale)) body.rationale = rationale;
    create.mutate(body, {
      onSuccess: (made) => void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: project.key, reqKey: made.key } }),
    });
  }

  if (!project.can.edit) return <p className="text-sm text-ink-muted">{t.requirements.readOnly}</p>;
  return (
    <div className="mx-auto max-w-3xl" data-new-requirement>
      <PageHeader title={t.requirements.createTitle} meta={t.projects.projectAndKey(project.name, project.key)} />
      <form onSubmit={submit} className="space-y-4" noValidate>
        {general && <ErrorBanner>{create.error.message}</ErrorBanner>}
        <Segmented
          label={t.requirements.kind}
          value={kind}
          onChange={setKind}
          options={[
            { value: "requirement", label: t.requirements.kinds.requirement, attrs: { "data-kind": "requirement" } },
            { value: "section", label: t.requirements.kinds.section, attrs: { "data-kind": "section" } },
          ]}
        />
        {kind === "section" && <p className="text-sm text-ink-muted">{t.requirements.sectionHint}</p>}
        <Field
          label={t.requirements.name}
          value={title}
          maxLength={REQUIREMENT_TITLE_MAX_LENGTH}
          onChange={(event) => setTitle(event.target.value)}
          error={fields.title}
          autoFocus
          required
        />
        <Labelled id="new-statement" label={t.requirements.statement} error={fields.statement}>
          <Editor id="new-statement" value={statement} onChange={setStatement} mentions={findPeople} aria-label={t.requirements.statement} />
        </Labelled>
        {kind === "requirement" && (
          <Labelled id="new-rationale" label={t.requirements.rationale} hint={t.requirements.rationaleHint} error={fields.rationale}>
            <Editor id="new-rationale" value={rationale} onChange={setRationale} mentions={findPeople} aria-label={t.requirements.rationale} />
          </Labelled>
        )}
        <Field
          label={t.requirements.comment}
          hint={t.requirements.commentHint}
          value={comment}
          maxLength={REVISION_COMMENT_MAX_LENGTH}
          onChange={(event) => setComment(event.target.value)}
          error={fields.comment}
        />
        <div className="flex gap-2">
          <Button type="submit" loading={create.isPending} data-action="create-requirement">
            {t.requirements.create}
          </Button>
          <Button type="button" variant="secondary" onClick={() => navigate({ to: "/p/$projectKey", params: { projectKey: project.key } })}>
            {t.requirements.cancel}
          </Button>
        </div>
      </form>
    </div>
  );
}
