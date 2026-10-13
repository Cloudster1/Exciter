import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import {
  PRIORITIES,
  findPeople,
  useTrashRequirement,
  useUpdateRequirement,
  type Priority,
  type Requirement,
  type RequirementChanges,
} from "@/api/requirements";
import { useAttributes, useTypes } from "@/api/attributes";
import { Button, Card, ErrorBanner, Field, IconButton, Labelled, PageHeader, SectionTitle, Select, SelectInput, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REQUIREMENT_TITLE_MAX_LENGTH, REVISION_COMMENT_MAX_LENGTH } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import { emptyDoc, isEmptyDoc, type Doc } from "@/features/editor/schema";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { AttributeFields, AttributeValues, valuesFor, type Values } from "@/features/types/AttributeFields";
import { TypeIcon } from "@/features/types/TypeIcon";
import { RequirementLabels } from "@/features/labels/RequirementLabels";
import { LinksPanel } from "@/features/links/LinksPanel";
import { PlaceDialog } from "@/features/tree/PlaceDialog";
import { TransitionBar } from "@/features/workflows/TransitionBar";
import { t } from "@/i18n";

/** One requirement or section: what it says, read or edited in place, and who owns it at which priority. */
export function RequirementPage({ project, requirement }: { project: Project; requirement: Requirement }) {
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState("");
  const mayEdit = requirement.can.edit;
  const section = requirement.kind === "section";
  return (
    <div className="mx-auto max-w-3xl space-y-5" data-requirement={requirement.key}>
      <PageHeader
        crumb={
          <Link to="/p/$projectKey" params={{ projectKey: project.key }} className="hover:text-accent hover:underline">
            {t.requirements.backToList}
          </Link>
        }
        title={requirement.title}
        meta={
          <span className="flex flex-wrap items-center gap-2">
            <span className="font-mono" data-requirement-key>
              {requirement.key}
            </span>
            {section && <Tag>{t.requirements.kinds.section}</Tag>}
            {requirement.type && (
              <span className="flex items-center gap-1" data-requirement-type={requirement.type.name}>
                <TypeIcon icon={requirement.type.icon} colour={requirement.type.colour} />
                {requirement.type.name}
              </span>
            )}
            <span data-revision={requirement.revision}>{t.requirements.revision(requirement.revision)}</span>
            {requirement.outline && <span data-outline={requirement.outline}>{requirement.outline}</span>}
            <Link
              to="/p/$projectKey/r/$reqKey/history"
              params={{ projectKey: project.key, reqKey: requirement.key }}
              search={{}}
              className="hover:text-accent hover:underline"
              data-history-link=""
            >
              {t.history.link}
            </Link>
          </span>
        }
        actions={
          mayEdit &&
          !editing && (
            <Button variant="secondary" icon={<Icon.Edit />} onClick={() => setEditing(true)} data-action="edit-requirement">
              {t.requirements.edit}
            </Button>
          )
        }
      />
      {!section && <TransitionBar requirement={requirement} />}
      {requirement.trashedAt && (
        <p role="status" className="rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-trashed-notice>
          {t.requirements.trashed}
        </p>
      )}
      {!mayEdit && !requirement.trashedAt && <p className="text-sm text-ink-muted">{t.requirements.readOnly}</p>}
      {requirement.copiedFrom && (
        <p className="text-sm text-ink-muted" data-copied-from>
          <Link
            to="/p/$projectKey/r/$reqKey"
            params={{ projectKey: requirement.copiedFrom.projectKey, reqKey: requirement.copiedFrom.key }}
            className="hover:text-accent hover:underline"
          >
            {t.tree.copiedFrom(requirement.copiedFrom.key)}
          </Link>
        </p>
      )}
      {requirement.formerKeys.length > 0 && (
        <p className="text-sm text-ink-muted" data-former-keys>
          {t.requirements.formerKeys(requirement.formerKeys.join(", "))}
        </p>
      )}
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-requirement-notice>
        {notice}
      </p>
      {editing ? (
        <RequirementEdit
          key={requirement.id}
          project={project}
          requirement={requirement}
          onDone={(message) => {
            setNotice(message);
            setEditing(false);
          }}
          onCancel={() => setEditing(false)}
        />
      ) : (
        <Content project={project} requirement={requirement} />
      )}
      {!section && <Facts requirement={requirement} />}
      <RequirementLabels requirement={requirement} />
      <LinksPanel requirement={requirement} />
      {mayEdit && <TrashSection project={project} requirement={requirement} />}
    </div>
  );
}

function Content({ project, requirement }: { project: Project; requirement: Requirement }) {
  const { data: attributes } = useAttributes(project.key);
  return (
    <Card className="space-y-4 p-4">
      <section aria-labelledby="statement-title" className="space-y-2">
        <SectionTitle id="statement-title">{t.requirements.statement}</SectionTitle>
        <DocView doc={requirement.statement as Doc} data-statement="" />
      </section>
      {requirement.kind === "requirement" && (
        <section aria-labelledby="rationale-title" className="space-y-2">
          <SectionTitle id="rationale-title">{t.requirements.rationale}</SectionTitle>
          {requirement.rationale ? (
            <DocView doc={requirement.rationale as Doc} data-rationale="" />
          ) : (
            <p className="text-sm text-ink-subtle">{t.requirements.noRationale}</p>
          )}
        </section>
      )}
      {attributes && <AttributeValues attributes={attributes} values={requirement.attributes as Values} />}
    </Card>
  );
}

/** Changes what the requirement says over the revision it was opened at; a newer one refuses the save, and the draft stays. */
export function RequirementEdit({
  project,
  requirement,
  onDone,
  onCancel,
}: {
  project: Project;
  requirement: Requirement;
  onDone: (notice: string) => void;
  onCancel: () => void;
}) {
  const update = useUpdateRequirement(requirement.key);
  const { data: types } = useTypes(project.key);
  const { data: attributes } = useAttributes(project.key);
  const [typeId, setTypeId] = useState<string | null>(requirement.type?.id ?? null);
  const [values, setValues] = useState<Values>((requirement.attributes as Values) ?? {});
  const [title, setTitle] = useState(requirement.title);
  const [statement, setStatement] = useState<Doc>((requirement.statement as Doc | null) ?? emptyDoc);
  const [rationale, setRationale] = useState<Doc | null>((requirement.rationale as Doc | null) ?? null);
  const [comment, setComment] = useState("");
  const conflict = update.error instanceof ApiError && update.error.code === "revision_conflict";
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  const general = update.error && Object.keys(fields).length === 0;

  // The revision the draft was opened at; after a refusal, the newer one the author chose to save over.
  const [base, setBase] = useState(requirement.revision);

  function save(event?: FormEvent, over = base) {
    event?.preventDefault();
    setBase(over);
    const changes: RequirementChanges = { title, statement, baseRevision: over, comment };
    if (requirement.kind === "requirement") {
      changes.rationale = rationale && !isEmptyDoc(rationale) ? rationale : null;
      if (typeId && typeId !== requirement.type?.id) changes.typeId = typeId;
      // Only what changed is sent, so a value cleared is sent as null and the rest stay as they are.
      const before = (requirement.attributes as Values) ?? {};
      const changed = Object.fromEntries(
        Object.entries(values)
          .filter(([id, value]) => JSON.stringify(value ?? null) !== JSON.stringify(before[id] ?? null))
          .map(([id, value]) => [id, value ?? null]),
      );
      if (Object.keys(changed).length > 0) changes.attributes = changed;
    }
    update.mutate(changes, {
      onSuccess: (saved) => onDone(saved.revision > requirement.revision ? t.requirements.saved(saved.revision) : t.requirements.savedNoRevision),
    });
  }

  return (
    <Card className="p-4">
      <form onSubmit={save} className="space-y-4" noValidate data-requirement-edit>
        {general && <ErrorBanner>{update.error.message}</ErrorBanner>}
        {conflict && (
          <div className="flex flex-wrap items-center gap-2 text-sm text-ink-muted" data-conflict>
            <span>{t.requirements.conflictReloaded}</span>
            <Button variant="secondary" size="sm" onClick={() => save(undefined, requirement.revision)} data-action="save-over-latest">
              {t.requirements.saveOver(requirement.revision)}
            </Button>
          </div>
        )}
        <Field
          label={t.requirements.name}
          value={title}
          maxLength={REQUIREMENT_TITLE_MAX_LENGTH}
          onChange={(event) => setTitle(event.target.value)}
          error={fields.title}
          required
        />
        <Labelled id="edit-statement" label={t.requirements.statement} error={fields.statement}>
          <Editor id="edit-statement" value={statement} onChange={setStatement} mentions={findPeople} aria-label={t.requirements.statement} />
        </Labelled>
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
        {requirement.kind === "requirement" && (
          <Labelled id="edit-rationale" label={t.requirements.rationale} hint={t.requirements.rationaleHint} error={fields.rationale}>
            <Editor id="edit-rationale" value={rationale} onChange={setRationale} mentions={findPeople} aria-label={t.requirements.rationale} />
          </Labelled>
        )}
        {requirement.kind === "requirement" && attributes && (
          <AttributeFields attributes={attributes} typeId={typeId} values={values} onChange={setValues} errors={fields} />
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

/** The owner and the priority, which are not what the requirement says and change at once, without a revision. */
function Facts({ requirement }: { requirement: Requirement }) {
  const update = useUpdateRequirement(requirement.key);
  const mayEdit = requirement.can.edit;
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  return (
    <Card className="grid gap-4 p-4 sm:grid-cols-2" data-requirement-facts>
      <div className="space-y-2">
        <SectionTitle>{t.requirements.owner}</SectionTitle>
        <p className="flex items-center gap-2 text-sm text-ink" data-owner>
          {requirement.owner?.name ?? <span className="text-ink-subtle">{t.requirements.noOwner}</span>}
          {mayEdit && requirement.owner && (
            <IconButton
              icon={<Icon.X />}
              label={t.requirements.removeOwner}
              size="xs"
              onClick={() => update.mutate({ ownerId: null })}
              data-action="remove-owner"
            />
          )}
        </p>
        {mayEdit && <SubjectPicker label={t.requirements.pickOwner} peopleOnly onPick={(person) => update.mutate({ ownerId: person.id })} />}
        {fields.ownerId && <p className="text-sm text-danger">{fields.ownerId}</p>}
      </div>
      <div className="space-y-2">
        <SectionTitle>{t.requirements.priority}</SectionTitle>
        <SelectInput
          value={requirement.priority ?? ""}
          disabled={!mayEdit || update.isPending}
          aria-label={t.requirements.priority}
          onChange={(event) => update.mutate({ priority: event.target.value === "" ? null : (event.target.value as Priority) })}
          data-priority-select
        >
          <option value="">{t.requirements.noPriority}</option>
          {PRIORITIES.map((priority) => (
            <option key={priority} value={priority}>
              {t.requirements.priorities[priority]}
            </option>
          ))}
        </SelectInput>
        {update.error && !fields.ownerId && <ErrorBanner>{update.error.message}</ErrorBanner>}
      </div>
    </Card>
  );
}

function TrashSection({ project, requirement }: { project: Project; requirement: Requirement }) {
  const navigate = useNavigate();
  const trash = useTrashRequirement(requirement.key, project.key);
  const [placing, setPlacing] = useState<"move" | "copy" | null>(null);
  const placed = { id: requirement.id, key: requirement.key, title: requirement.title, projectKey: project.key };
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="secondary" onClick={() => setPlacing("move")} data-action="move-requirement">
        {t.tree.move}
      </Button>
      <Button variant="secondary" onClick={() => setPlacing("copy")} data-action="copy-requirement">
        {t.tree.copy}
      </Button>
      {placing && (
        <PlaceDialog
          placed={placed}
          mode={placing}
          onClose={() => setPlacing(null)}
          onDone={(r) => {
            setPlacing(null);
            void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: r.projectKey, reqKey: r.key } });
          }}
        />
      )}
      <Button
        variant="secondary"
        icon={<Icon.Trash />}
        loading={trash.isPending}
        onClick={() => trash.mutate(undefined, { onSuccess: () => void navigate({ to: "/p/$projectKey", params: { projectKey: project.key } }) })}
        data-action="trash-requirement"
      >
        {t.requirements.trash}
      </Button>
      {trash.error && <ErrorBanner>{trash.error.message}</ErrorBanner>}
    </div>
  );
}
