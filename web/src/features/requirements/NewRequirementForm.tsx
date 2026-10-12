import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { findPeople, useCreateRequirement, type NewRequirement } from "@/api/requirements";
import { useAttributes, useTypes } from "@/api/attributes";
import { Button, ErrorBanner, Field, Labelled, PageHeader, Segmented, Select } from "@/components/ui";
import { REQUIREMENT_TITLE_MAX_LENGTH, REVISION_COMMENT_MAX_LENGTH } from "@/config";
import { Editor } from "@/features/editor/Editor";
import { isEmptyDoc, type Doc } from "@/features/editor/schema";
import { AttributeFields, valuesFor, type Values } from "@/features/types/AttributeFields";
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
  const { data: types } = useTypes(project.key);
  const { data: attributes } = useAttributes(project.key);
  const [typeId, setTypeId] = useState<string | null>(project.settings.defaultTypeId ?? null);
  const [values, setValues] = useState<Values>({});
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const general = create.error && Object.keys(fields).length === 0;

  function submit(event: FormEvent) {
    event.preventDefault();
    const body: NewRequirement = { kind, title, comment };
    if (statement && !isEmptyDoc(statement)) body.statement = statement;
    if (kind === "requirement" && rationale && !isEmptyDoc(rationale)) body.rationale = rationale;
    if (kind === "requirement") {
      if (typeId) body.typeId = typeId;
      const given = Object.fromEntries(Object.entries(values).filter(([, value]) => value !== null && value !== undefined));
      if (Object.keys(given).length > 0) body.attributes = given;
    }
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
          <Select
            label={t.types.type}
            value={typeId ?? ""}
            onChange={(event) => {
              const next = event.target.value || null;
              setTypeId(next);
              setValues(valuesFor(attributes ?? [], next, values));
            }}
            error={fields.typeId}
            data-type-select
          >
            {(types ?? [])
              .filter((type) => !type.retired)
              .map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
          </Select>
        )}
        {kind === "requirement" && (
          <Labelled id="new-rationale" label={t.requirements.rationale} hint={t.requirements.rationaleHint} error={fields.rationale}>
            <Editor id="new-rationale" value={rationale} onChange={setRationale} mentions={findPeople} aria-label={t.requirements.rationale} />
          </Labelled>
        )}
        {kind === "requirement" && attributes && (
          <AttributeFields attributes={attributes} typeId={typeId} values={values} onChange={setValues} errors={fields} />
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
