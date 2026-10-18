import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { findPeople, useCreateRequirement, type NewRequirement, type Requirement } from "@/api/requirements";
import type { Place } from "@/api/tree";
import { useAttributes, useTypes } from "@/api/attributes";
import { Button, ErrorBanner, Field, Labelled, PageHeader, Segmented, Select } from "@/components/ui";
import { REQUIREMENT_TITLE_MAX_LENGTH, REVISION_COMMENT_MAX_LENGTH } from "@/config";
import { Editor, type EditorControls } from "@/features/editor/Editor";
import { emptyDoc, isEmptyDoc, type Doc } from "@/features/editor/schema";
import { FindingsList } from "@/features/quality/FindingsList";
import { useDraftQuality } from "@/features/quality/draft";
import { SimilarHint } from "@/features/duplicates/SimilarHint";
import { PlaceDialog, type Placed } from "@/features/tree/PlaceDialog";
import { AttributeFields, valuesFor, type Values } from "@/features/types/AttributeFields";
import { t } from "@/i18n";
import { TestCaseFields } from "@/features/testcases/TestCaseFields";
import { contentOf, draftOf, holdsAnything, type TestCaseDraft } from "@/features/testcases/draft";

type Kind = NonNullable<NewRequirement["kind"]>;

/** Writes a requirement or a section as revision 1, and opens it. */
export function NewRequirementForm({ project }: { project: Project }) {
  const navigate = useNavigate();
  if (!project.can.edit) return <p className="text-sm text-ink-muted">{t.requirements.readOnly}</p>;
  return (
    <div className="mx-auto max-w-3xl" data-new-requirement>
      <PageHeader title={t.requirements.createTitle} meta={t.projects.projectAndKey(project.name, project.key)} />
      <RequirementCreate
        project={project}
        onCreated={(made) => void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: project.key, reqKey: made.key } })}
        onCancel={() => void navigate({ to: "/p/$projectKey", params: { projectKey: project.key } })}
      />
    </div>
  );
}

/** The fields of a new requirement or section, written where place says, or last at the top. */
export function RequirementCreate({
  project,
  place,
  onCreated,
  onCancel,
  onDirty,
}: {
  project: Project;
  place?: Place;
  onCreated: (made: Requirement) => void;
  onCancel: () => void;
  /** Hears whether anything was written yet. */
  onDirty?: (dirty: boolean) => void;
}) {
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
  const [testCase, setTestCase] = useState<TestCaseDraft>(() => draftOf(null));
  const isTest = kind === "requirement" && (types ?? []).some((type) => type.id === typeId && type.role === "testCase");
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const general = create.error && Object.keys(fields).length === 0;
  const dirty =
    title !== "" || comment !== "" || statement !== null || rationale !== null || Object.keys(values).length > 0 || (isTest && holdsAnything(testCase));
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  const editorControls = useRef<EditorControls>(null);
  const written = title.trim() !== "" || !isEmptyDoc(statement);
  // A library's requirement the draft is like may be reused in its place (#208, #49).
  const [reusing, setReusing] = useState<Placed | null>(null);
  const quality = useDraftQuality(
    project,
    { title, statement: statement ?? emptyDoc, ...(kind === "requirement" && typeId ? { typeId } : {}) },
    kind === "requirement" && written,
  );

  function submit(event?: FormEvent) {
    event?.preventDefault();
    const body: NewRequirement = { kind, title, comment };
    if (place) body.place = place;
    if (statement && !isEmptyDoc(statement)) body.statement = statement;
    if (kind === "requirement" && rationale && !isEmptyDoc(rationale)) body.rationale = rationale;
    if (kind === "requirement") {
      if (typeId) body.typeId = typeId;
      const given = Object.fromEntries(Object.entries(values).filter(([, value]) => value !== null && value !== undefined));
      if (Object.keys(given).length > 0) body.attributes = given;
      if (isTest && holdsAnything(testCase)) body.testCase = contentOf(testCase);
    }
    create.mutate(body, { onSuccess: onCreated });
  }

  // Ctrl or Cmd with S writes it from anywhere in the form, and Escape leaves it, unless the editor's own popup took it.
  function keys(event: KeyboardEvent<HTMLFormElement>) {
    if (event.defaultPrevented) return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
      event.preventDefault();
      if (!create.isPending) submit();
    } else if (event.key === "Escape") {
      event.preventDefault();
      onCancel();
    }
  }

  return (
    <>
      <form onSubmit={submit} onKeyDown={keys} className="space-y-4" noValidate data-requirement-create>
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
          <Editor
            id="new-statement"
            value={statement}
            onChange={setStatement}
            mentions={findPeople}
            findings={quality.marks}
            terms={quality.terms}
            glossary={project.key}
            controls={editorControls}
            aria-label={t.requirements.statement}
          />
        </Labelled>
        {quality.on && (
          <FindingsList
            findings={quality.check?.findings ?? []}
            sentences={quality.check?.sentences}
            score={quality.check?.score}
            checking={!quality.current && !quality.failed}
            failed={quality.failed}
            onShow={(finding) => editorControls.current?.showFinding(finding)}
          />
        )}
        <SimilarHint
          project={project}
          title={title}
          statement={statement ?? emptyDoc}
          enabled={kind === "requirement" && written}
          onReuse={(hit) => setReusing({ ...hit.requirement })}
        />
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
        {isTest && <TestCaseFields value={testCase} onChange={setTestCase} errors={fields} />}
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
          <Button type="button" variant="secondary" onClick={onCancel}>
            {t.requirements.cancel}
          </Button>
        </div>
      </form>
      {/* Outside the form, whose submit and keys a dialog's own would otherwise reach through the portal. */}
      {reusing && <PlaceDialog placed={reusing} mode="reuse" into={project.key} onClose={() => setReusing(null)} onDone={onCreated} />}
    </>
  );
}
