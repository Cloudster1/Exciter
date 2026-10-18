import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { PRIORITIES, useTrashRequirement, useUpdateRequirement, type Priority, type Requirement } from "@/api/requirements";
import { useAttributes } from "@/api/attributes";
import { useQuality } from "@/api/quality";
import { Button, Card, ErrorBanner, IconButton, PageHeader, SectionTitle, SelectInput, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { AttributeValues, type Values } from "@/features/types/AttributeFields";
import { TypeIcon } from "@/features/types/TypeIcon";
import { RequirementLabels } from "@/features/labels/RequirementLabels";
import { AttachmentPanel } from "@/features/attachments/AttachmentPanel";
import { ArmatureLinks } from "@/features/armature/ArmatureLinks";
import { StatorLinks } from "@/features/stator/StatorLinks";
import { TestResults } from "@/features/evidence/TestResults";
import { FindingsList, findingMarks } from "@/features/quality/FindingsList";
import { QualityBadge } from "@/features/quality/QualityBadge";
import { CodeLinks } from "@/features/evidence/CodeLinks";
import { TermsPanel } from "@/features/glossary/TermsPanel";
import { SimilarPanel } from "@/features/duplicates/SimilarPanel";
import { CommentsSection } from "@/features/comments/CommentsSection";
import { ReviewPanel } from "@/features/reviews/ReviewPanel";
import { LinksPanel } from "@/features/links/LinksPanel";
import { ReuseBanner } from "@/features/reuse/ReuseBanner";
import { PlaceDialog } from "@/features/tree/PlaceDialog";
import { TransitionBar } from "@/features/workflows/TransitionBar";
import { WatchMenu } from "@/features/watching/WatchMenu";
import { t } from "@/i18n";
import { useFieldAccess } from "@/features/types/fieldAccess";
import { LockAction, LockBanner } from "./LockBanner";
import { RequirementEdit } from "./RequirementEdit";

/** One requirement or section: what it says, read or edited in place, and who owns it at which priority. */
export function RequirementPage({ project, requirement }: { project: Project; requirement: Requirement }) {
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState("");
  const [watchFailed, setWatchFailed] = useState(false);
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
            {!section && <QualityBadge score={requirement.quality.score} stale={requirement.quality.stale} />}
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
            {!section && (
              <Link
                to="/p/$projectKey/r/$reqKey/trace"
                params={{ projectKey: project.key, reqKey: requirement.key }}
                search={{}}
                className="hover:text-accent hover:underline"
                data-graph-link=""
              >
                {t.graph.link}
              </Link>
            )}
          </span>
        }
        actions={
          <>
            <WatchMenu requirement={requirement} onFailure={setWatchFailed} />
            <LockAction project={project} requirement={requirement} onNotice={setNotice} />
            {mayEdit && !editing && (
              <Button variant="secondary" icon={<Icon.Edit />} onClick={() => setEditing(true)} data-action="edit-requirement">
                {t.requirements.edit}
              </Button>
            )}
          </>
        }
      />
      {watchFailed && <ErrorBanner>{t.watch.failed}</ErrorBanner>}
      {!section && <TransitionBar requirement={requirement} />}
      {requirement.trashedAt && (
        <p role="status" className="rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-trashed-notice>
          {t.requirements.trashed}
        </p>
      )}
      <LockBanner project={project} requirement={requirement} onNotice={setNotice} />
      {!mayEdit && !requirement.trashedAt && !requirement.lock && <p className="text-sm text-ink-muted">{t.requirements.readOnly}</p>}
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
      <ReuseBanner requirement={requirement} onNotice={setNotice} />
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
      {!section && <Facts project={project} requirement={requirement} />}
      {!section && <ReviewPanel requirement={requirement} />}
      <RequirementLabels requirement={requirement} />
      <LinksPanel requirement={requirement} />
      <TermsPanel project={project} requirement={requirement} />
      <SimilarPanel requirement={requirement} />
      <ArmatureLinks requirement={requirement} />
      <StatorLinks requirement={requirement} />
      <TestResults requirement={requirement} />
      <CodeLinks requirement={requirement} />
      <AttachmentPanel requirement={requirement} />
      <CommentsSection requirement={requirement} />
      {project.settings.library && !requirement.trashedAt && <ReuseAction project={project} requirement={requirement} />}
      {mayEdit && <TrashSection project={project} requirement={requirement} />}
    </div>
  );
}

function Content({ project, requirement }: { project: Project; requirement: Requirement }) {
  const { data: attributes } = useAttributes(project.key);
  // Findings are read only when asked for, so a requirement read for its words reads nothing more (#207).
  const [showFindings, setShowFindings] = useState(false);
  const checked = requirement.kind === "requirement";
  const { data: quality, error } = useQuality(requirement.key, requirement.revision, checked && showFindings);
  const may = requirement.can.edit && !project.archivedAt && !requirement.trashedAt;
  return (
    <Card className="space-y-4 p-4">
      <section aria-labelledby="statement-title" className="space-y-2">
        <div className="flex items-center justify-between gap-2">
          <SectionTitle id="statement-title">{t.requirements.statement}</SectionTitle>
          {checked && (
            <Button variant="ghost" size="sm" aria-pressed={showFindings} onClick={() => setShowFindings(!showFindings)} data-action="show-findings">
              {t.quality.showFindings}
            </Button>
          )}
        </div>
        <DocView doc={requirement.statement as Doc} findings={showFindings && quality ? findingMarks(quality.findings) : undefined} data-statement="" />
        {showFindings && error && <ErrorBanner>{error.message}</ErrorBanner>}
        {showFindings && quality && (
          <FindingsList findings={quality.findings} sentences={quality.sentences} accept={{ reqKey: requirement.key, revision: quality.revision, may }} />
        )}
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

/** The owner and the priority, which are not what the requirement says and change at once, without a revision. */
function Facts({ project, requirement }: { project: Project; requirement: Requirement }) {
  const update = useUpdateRequirement(requirement.key);
  // A lock keeps what the requirement says, not who owns it or its priority (#205); the field rules may (#206).
  const editor = project.can.edit && !project.archivedAt && !requirement.trashedAt;
  const access = useFieldAccess(project, requirement.status);
  const owner = access("owner");
  const priority = access("priority");
  const mayOwn = editor && owner.edit;
  const mayPrioritize = editor && priority.edit;
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  return (
    <Card className="grid gap-4 p-4 sm:grid-cols-2" data-requirement-facts>
      <div className="space-y-2">
        <SectionTitle>{t.requirements.owner}</SectionTitle>
        <p className="flex items-center gap-2 text-sm text-ink" data-owner>
          {requirement.owner?.name ?? <span className="text-ink-subtle">{t.requirements.noOwner}</span>}
          {mayOwn && requirement.owner && (
            <IconButton
              icon={<Icon.X />}
              label={t.requirements.removeOwner}
              size="xs"
              onClick={() => update.mutate({ ownerId: null })}
              data-action="remove-owner"
            />
          )}
        </p>
        {mayOwn && <SubjectPicker label={t.requirements.pickOwner} peopleOnly onPick={(person) => update.mutate({ ownerId: person.id })} />}
        {editor && !owner.edit && (
          <p className="text-xs text-ink-muted" data-read-only-why="owner">
            {owner.why}
          </p>
        )}
        {fields.ownerId && <p className="text-sm text-danger">{fields.ownerId}</p>}
      </div>
      <div className="space-y-2">
        <SectionTitle>{t.requirements.priority}</SectionTitle>
        <SelectInput
          value={requirement.priority ?? ""}
          disabled={!mayPrioritize || update.isPending}
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
        {editor && !priority.edit && (
          <p className="text-xs text-ink-muted" data-read-only-why="priority">
            {priority.why}
          </p>
        )}
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
        onClick={() =>
          // The refetch that shows the requirement trashed unmounts these actions, which drops a mutate call's own
          // onSuccess; the promise resolves regardless.
          void trash
            .mutateAsync()
            .then(() => navigate({ to: "/p/$projectKey", params: { projectKey: project.key } }))
            .catch(() => {})
        }
        data-action="trash-requirement"
      >
        {t.requirements.trash}
      </Button>
      {trash.error && <ErrorBanner>{trash.error.message}</ErrorBanner>}
    </div>
  );
}

/** Offers a library requirement to the other projects the reader edits (#49). */
function ReuseAction({ project, requirement }: { project: Project; requirement: Requirement }) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const placed = { id: requirement.id, key: requirement.key, title: requirement.title, projectKey: project.key };
  return (
    <div>
      <Button variant="secondary" onClick={() => setOpen(true)} data-action="reuse-requirement">
        {t.reuse.reuse}
      </Button>
      {open && (
        <PlaceDialog
          placed={placed}
          mode="reuse"
          onClose={() => setOpen(false)}
          onDone={(r) => {
            setOpen(false);
            void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: r.projectKey, reqKey: r.key } });
          }}
        />
      )}
    </div>
  );
}
