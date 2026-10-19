import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { ApiError } from "@/api/client";
import { useComparison } from "@/api/history";
import type { Project } from "@/api/projects";
import { findPeople, useUpdateRequirement, type Requirement, type RequirementChanges } from "@/api/requirements";
import { useUploadAttachments } from "@/api/attachments";
import { useQuality } from "@/api/quality";
import { useAttributes, useTypes } from "@/api/attributes";
import { Button, Card, ErrorBanner, Field, Labelled, SectionTitle, Select, Skeleton } from "@/components/ui";
import { REQUIREMENT_TITLE_MAX_LENGTH, REVISION_COMMENT_MAX_LENGTH } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { Editor, type EditorControls } from "@/features/editor/Editor";
import { emptyDoc, isEmptyDoc, type Doc } from "@/features/editor/schema";
import { ComparisonView } from "@/features/history/RequirementHistory";
import { FindingsList } from "@/features/quality/FindingsList";
import { useDraftQuality } from "@/features/quality/draft";
import { SimilarHint } from "@/features/duplicates/SimilarHint";
import { AttributeFields, valuesFor, type Values } from "@/features/types/AttributeFields";
import { t } from "@/i18n";
import { useFieldAccess } from "@/features/types/fieldAccess";
import { TestCaseFields } from "@/features/testcases/TestCaseFields";
import { contentOf, draftOf, holdsAnything, type TestCaseDraft } from "@/features/testcases/draft";
import { RatingFields } from "@/features/risks/RatingFields";
import { draftRating, rated } from "@/features/risks/level";
import { riskError } from "@/features/risks/errors";
import type { Rating } from "@/api/risks";

/** What an edit starts from, which a requirement's page and a specification entry both hold. */
export type Editable = Pick<Requirement, "key" | "kind" | "title" | "statement" | "rationale" | "type" | "attributes" | "revision" | "reuse" | "status"> &
  Partial<Pick<Requirement, "computed" | "testCase" | "risk">>;

/** Changes what the requirement says over the revision it was opened at; a newer one refuses the save, and the draft stays. */
export function RequirementEdit({
  project,
  requirement,
  onDone,
  onCancel,
  onDirty,
  onConflict,
  autoFocus = false,
}: {
  project: Project;
  requirement: Editable;
  onDone: (notice: string, saved: Requirement) => void;
  onCancel: () => void;
  /** Hears whether the draft differs from what it was opened with. */
  onDirty?: (dirty: boolean) => void;
  /** Hears a refusal over a newer revision, so whoever shows the requirement reads it again. */
  onConflict?: () => void;
  autoFocus?: boolean;
}) {
  const update = useUpdateRequirement(requirement.key);
  const files = useUploadAttachments(requirement.key);
  const { data: types } = useTypes(project.key);
  const { data: attributes } = useAttributes(project.key);
  // What the reader may not change in the requirement's status shows as it is, with who does (#206).
  const access = useFieldAccess(project, requirement.status);
  const fixed = { title: access("title"), statement: access("statement"), rationale: access("rationale"), type: access("type") };
  const readOnly: Record<string, string> = {};
  for (const a of attributes ?? []) {
    const each = access(a.id);
    if (!each.edit) readOnly[a.id] = each.why;
  }
  const [typeId, setTypeId] = useState<string | null>(requirement.type?.id ?? null);
  const [values, setValues] = useState<Values>((requirement.attributes as Values) ?? {});
  const [title, setTitle] = useState(requirement.title);
  const [statement, setStatement] = useState<Doc>((requirement.statement as Doc | null) ?? emptyDoc);
  const [rationale, setRationale] = useState<Doc | null>((requirement.rationale as Doc | null) ?? null);
  const [comment, setComment] = useState("");
  const [testCase, setTestCase] = useState<TestCaseDraft>(() => draftOf(requirement.testCase));
  // A type marked as test cases gives its requirements steps (#250).
  const isTest = (types ?? []).some((type) => type.id === typeId && type.role === "testCase");
  const testOpened = useRef(JSON.stringify(contentOf(draftOf(requirement.testCase))));
  const testChanged = JSON.stringify(contentOf(testCase)) !== testOpened.current;
  const [rating, setRating] = useState<Rating>(() => draftRating(requirement.risk));
  // A type marked as risks gives its requirements a rating (#259).
  const isRisk = (types ?? []).some((type) => type.id === typeId && type.role === "risk");
  const ratingOpened = useRef(JSON.stringify(draftRating(requirement.risk)));
  const ratingChanged = JSON.stringify(rating) !== ratingOpened.current;
  const conflict = update.error instanceof ApiError && update.error.code === "revision_conflict";
  // A reuse that follows its library source takes its words from there, so only the rest is edited here (#49).
  const locked = requirement.reuse?.state === "following";
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  const general = update.error && Object.keys(fields).length === 0;

  // The revision the draft was opened at; after a refusal, the newer one the author chose to save over.
  const [base, setBase] = useState(requirement.revision);

  // What the draft was opened with, to tell an edit from a draft left as it was.
  const opened = useRef({ typeId, values, title, statement, rationale });
  const dirty =
    comment !== "" ||
    title !== opened.current.title ||
    typeId !== opened.current.typeId ||
    JSON.stringify(statement) !== JSON.stringify(opened.current.statement) ||
    JSON.stringify(rationale) !== JSON.stringify(opened.current.rationale) ||
    JSON.stringify(values) !== JSON.stringify(opened.current.values) ||
    (isTest && testChanged) ||
    (isRisk && ratingChanged);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  // The draft is checked as it is typed (#207); a finding the saved revision has too may be accepted from here.
  const checked = requirement.kind === "requirement" && !locked && fixed.statement.edit;
  const editorControls = useRef<EditorControls>(null);
  const quality = useDraftQuality(project, { title, statement, ...(typeId ? { typeId } : {}), requirementKey: requirement.key }, checked);
  const { data: saved } = useQuality(requirement.key, requirement.revision, quality.on);
  const savedKeys = useMemo(() => new Set((saved?.findings ?? []).map((f) => f.key)), [saved]);

  function save(event?: FormEvent, over = base) {
    event?.preventDefault();
    setBase(over);
    const changes: RequirementChanges = locked ? { baseRevision: over, comment } : { title, statement, baseRevision: over, comment };
    if (requirement.kind === "requirement") {
      if (!locked) changes.rationale = rationale && !isEmptyDoc(rationale) ? rationale : null;
      if (typeId && typeId !== requirement.type?.id) changes.typeId = typeId;
      // Only what changed is sent, so a value cleared is sent as null and the rest stay as they are.
      const before = (requirement.attributes as Values) ?? {};
      const changed = Object.fromEntries(
        Object.entries(values)
          .filter(([id, value]) => JSON.stringify(value ?? null) !== JSON.stringify(before[id] ?? null))
          .map(([id, value]) => [id, value ?? null]),
      );
      if (Object.keys(changed).length > 0) changes.attributes = changed;
      if (isTest && testChanged && !locked) changes.testCase = contentOf(testCase);
      if (isRisk && ratingChanged && !locked) changes.risk = rating;
    }
    update.mutate(changes, {
      onSuccess: (saved) => onDone(saved.revision > requirement.revision ? t.requirements.saved(saved.revision) : t.requirements.savedNoRevision, saved),
      onError: (error) => {
        if (error instanceof ApiError && error.code === "revision_conflict") onConflict?.();
      },
    });
  }

  // Ctrl or Cmd with S saves from anywhere in the form, and Escape leaves it, unless the editor's own popup took it.
  function keys(event: KeyboardEvent<HTMLFormElement>) {
    if (event.defaultPrevented) return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
      event.preventDefault();
      if (!update.isPending) save();
    } else if (event.key === "Escape") {
      event.preventDefault();
      onCancel();
    }
  }

  return (
    <Card className="p-4">
      <form onSubmit={save} onKeyDown={keys} className="space-y-4" noValidate data-requirement-edit>
        {general && <ErrorBanner>{update.error.message}</ErrorBanner>}
        {files.errors.map((message) => (
          <ErrorBanner key={message}>{message}</ErrorBanner>
        ))}
        {conflict && (
          <div className="space-y-3" data-conflict>
            <div className="flex flex-wrap items-center gap-2 text-sm text-ink-muted">
              <span>{t.requirements.conflictReloaded}</span>
              <Button variant="secondary" size="sm" onClick={() => save(undefined, requirement.revision)} data-action="save-over-latest">
                {t.requirements.saveOver(requirement.revision)}
              </Button>
            </div>
            {requirement.revision > base && <NewerRevision reqKey={requirement.key} from={base} to={requirement.revision} />}
          </div>
        )}
        {locked && (
          <p className="text-sm text-ink-muted" data-reuse-locked>
            {t.reuse.locked}
          </p>
        )}
        <Field
          label={t.requirements.name}
          value={title}
          maxLength={REQUIREMENT_TITLE_MAX_LENGTH}
          onChange={(event) => setTitle(event.target.value)}
          error={fields.title}
          hint={fixed.title.edit ? undefined : fixed.title.why}
          readOnly={locked || !fixed.title.edit}
          autoFocus={autoFocus}
          required
        />
        {locked || !fixed.statement.edit ? (
          <section aria-labelledby="locked-statement-title" className="space-y-2">
            <SectionTitle id="locked-statement-title">{t.requirements.statement}</SectionTitle>
            {!fixed.statement.edit && (
              <p className="text-xs text-ink-muted" data-read-only-why="statement">
                {fixed.statement.why}
              </p>
            )}
            <DocView doc={statement} data-statement="" />
          </section>
        ) : (
          <Labelled id="edit-statement" label={t.requirements.statement} error={fields.statement}>
            <Editor
              id="edit-statement"
              value={statement}
              onChange={setStatement}
              mentions={findPeople}
              upload={files.upload}
              findings={quality.marks}
              terms={quality.terms}
              glossary={project.key}
              controls={editorControls}
              aria-label={t.requirements.statement}
            />
          </Labelled>
        )}
        {quality.on && (
          <FindingsList
            findings={quality.check?.findings ?? []}
            sentences={quality.check?.sentences}
            score={quality.check?.score}
            checking={!quality.current && !quality.failed}
            failed={quality.failed}
            onShow={(finding) => editorControls.current?.showFinding(finding)}
            accept={{ reqKey: requirement.key, revision: requirement.revision, keys: savedKeys, may: Boolean(saved) }}
          />
        )}
        <SimilarHint project={project} title={title} statement={statement} excludeKey={requirement.key} enabled={checked} />
        {requirement.kind === "requirement" && (
          <Select
            label={t.types.type}
            value={typeId ?? ""}
            onChange={(event) => {
              const next = event.target.value || null;
              setTypeId(next);
              setValues(valuesFor(attributes ?? [], next, values));
            }}
            error={fields.typeId}
            disabled={!fixed.type.edit}
            hint={fixed.type.edit ? undefined : fixed.type.why}
            data-type-select
          >
            {(types ?? [])
              .filter((type) => !type.retired || type.id === requirement.type?.id)
              .map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
          </Select>
        )}
        {requirement.kind === "requirement" && !locked && !fixed.rationale.edit && (
          <section aria-labelledby="read-only-rationale-title" className="space-y-2">
            <SectionTitle id="read-only-rationale-title">{t.requirements.rationale}</SectionTitle>
            <p className="text-xs text-ink-muted" data-read-only-why="rationale">
              {fixed.rationale.why}
            </p>
            {rationale && <DocView doc={rationale} />}
          </section>
        )}
        {requirement.kind === "requirement" && !locked && fixed.rationale.edit && (
          <Labelled id="edit-rationale" label={t.requirements.rationale} hint={t.requirements.rationaleHint} error={fields.rationale}>
            <Editor
              id="edit-rationale"
              value={rationale}
              onChange={setRationale}
              mentions={findPeople}
              upload={files.upload}
              aria-label={t.requirements.rationale}
            />
          </Labelled>
        )}
        {requirement.kind === "requirement" && !locked && isTest && (
          <TestCaseFields value={testCase} onChange={setTestCase} errors={fields} upload={files.upload} />
        )}
        {requirement.kind === "requirement" && !locked && isRisk && (
          <RatingFields projectKey={project.key} value={rating} onChange={setRating} error={riskError(fields)} />
        )}
        {requirement.kind === "requirement" && !isRisk && typeId !== requirement.type?.id && rated(rating) && (
          <p role="status" className="text-sm text-ink-muted" data-rating-dropped="">
            {t.risks.dropped}
          </p>
        )}
        {requirement.kind === "requirement" && !isTest && typeId !== requirement.type?.id && holdsAnything(testCase) && (
          <p role="status" className="text-sm text-ink-muted" data-steps-dropped="">
            {t.testcases.dropped}
          </p>
        )}
        {requirement.kind === "requirement" && attributes && (
          <AttributeFields
            attributes={attributes}
            typeId={typeId}
            values={values}
            onChange={setValues}
            errors={fields}
            readOnly={readOnly}
            computed={requirement.computed as Values}
          />
        )}
        <Field
          label={t.requirements.comment}
          hint={t.requirements.commentHint}
          value={comment}
          maxLength={REVISION_COMMENT_MAX_LENGTH}
          onChange={(event) => setComment(event.target.value)}
          error={fields.comment ?? fields.baseRevision}
        />
        <div className="flex gap-2">
          <Button type="submit" loading={update.isPending} data-action="save-requirement">
            {t.requirements.save}
          </Button>
          <Button type="button" variant="secondary" onClick={onCancel}>
            {t.requirements.cancel}
          </Button>
        </div>
      </form>
    </Card>
  );
}

/** What the newer revision changed since the draft was opened, to read before saving over it. */
function NewerRevision({ reqKey, from, to }: { reqKey: string; from: number; to: number }) {
  const { data: comparison } = useComparison(reqKey, from, to);
  return (
    <section aria-label={t.requirements.newerChanges(to)} className="rounded-control border border-border p-3" data-newer-revision={to}>
      <p className="mb-2 text-sm font-medium text-ink">{t.requirements.newerChanges(to)}</p>
      {comparison ? <ComparisonView comparison={comparison} /> : <Skeleton />}
    </section>
  );
}
