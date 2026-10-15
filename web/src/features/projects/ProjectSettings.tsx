import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useTypes } from "@/api/attributes";
import { useChangeProjectKey, useDeleteProject, useSetArchived, useUpdateProject, type Project, type ProjectChanges } from "@/api/projects";
import { Button, Card, Checkbox, Dialog, ErrorBanner, Field, PageHeader, SectionTitle, Select, TabPanel, Tabs } from "@/components/ui";
import { ImportPanel } from "@/features/import/ImportPanel";
import { ReqIFPanel } from "@/features/reqif/ReqIFPanel";
import { ApprovalRules } from "@/features/reviews/ApprovalRules";
import { ProjectLinkTypes } from "@/features/links/LinkTypeList";
import { AttributeList } from "@/features/types/AttributeList";
import { TypeList } from "@/features/types/TypeList";
import { WorkflowSettings } from "@/features/workflows/WorkflowSettings";
import { PROJECTS_PATH, PROJECT_DESCRIPTION_MAX_LENGTH, PROJECT_KEY_MAX_LENGTH, PROJECT_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

const SETTINGS_TABS = ["details", "types", "attributes", "workflow", "approval", "links", "import", "reqif"] as const;
export type SettingsTab = (typeof SETTINGS_TABS)[number];

export function isSettingsTab(value: unknown): value is SettingsTab {
  return typeof value === "string" && (SETTINGS_TABS as readonly string[]).includes(value);
}

/** A project's details, its key, its types, attributes, workflows and link types, archiving and deleting it; changing
 * anything is its administrators'. */
export function ProjectSettings({ project, tab: opened }: { project: Project; tab?: SettingsTab }) {
  const readOnly = !project.can.administer;
  const [tab, setTab] = useState<SettingsTab>(opened ?? "details");
  return (
    <div className="mx-auto max-w-3xl space-y-6" data-project-settings={project.key}>
      <PageHeader
        title={t.projects.settingsTitle}
        meta={t.projects.projectAndKey(project.name, project.key)}
        tabs={
          <Tabs<SettingsTab>
            label={t.projects.settingsTitle}
            value={tab}
            onChange={setTab}
            panelId="project-settings-panel"
            tabs={[
              { value: "details", label: t.projects.detailsTitle, attrs: { "data-tab": "details" } },
              { value: "types", label: t.types.tab, attrs: { "data-tab": "types" } },
              { value: "attributes", label: t.attributes.tab, attrs: { "data-tab": "attributes" } },
              { value: "workflow", label: t.workflows.tab, attrs: { "data-tab": "workflow" } },
              { value: "approval", label: t.review.rulesTab, attrs: { "data-tab": "approval" } },
              { value: "links", label: t.linkTypes.tab, attrs: { "data-tab": "links" } },
              { value: "import", label: t.imports.tab, attrs: { "data-tab": "import" } },
              { value: "reqif", label: t.reqif.tab, attrs: { "data-tab": "reqif" } },
            ]}
          />
        }
      />
      {project.archivedAt && (
        <p role="status" className="rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-archived-notice>
          {t.projects.archivedNotice}
        </p>
      )}
      {readOnly && <p className="text-sm text-ink-muted">{t.projects.notProjectAdmin}</p>}
      <TabPanel id="project-settings-panel" className="space-y-6">
        {tab === "details" && (
          <>
            <Details key={`${project.id}-${project.updatedAt}`} project={project} readOnly={readOnly || Boolean(project.archivedAt)} />
            <KeySection project={project} readOnly={readOnly || Boolean(project.archivedAt)} />
            {!readOnly && <ArchiveSection project={project} />}
            {!readOnly && project.archivedAt && <DeleteSection project={project} />}
          </>
        )}
        {tab === "types" && <TypeList project={project} />}
        {tab === "attributes" && <AttributeList project={project} />}
        {tab === "workflow" && <WorkflowSettings project={project} />}
        {tab === "approval" && <ApprovalRules project={project} />}
        {tab === "links" && <ProjectLinkTypes project={project} />}
        {tab === "import" && <ImportPanel project={project} />}
        {tab === "reqif" && <ReqIFPanel project={project} />}
      </TabPanel>
    </div>
  );
}

function Details({ project, readOnly }: { project: Project; readOnly: boolean }) {
  const update = useUpdateProject(project.key);
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [requireComment, setRequireComment] = useState(project.settings.requireRevisionComment);
  const [defaultType, setDefaultType] = useState(project.settings.defaultTypeId ?? "");
  const { data: types } = useTypes(project.key);
  const [notice, setNotice] = useState("");
  const fields = update.error instanceof ApiError ? update.error.fields : {};

  function save(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    const settings: ProjectChanges["settings"] = { requireRevisionComment: requireComment };
    if (defaultType && defaultType !== project.settings.defaultTypeId) settings.defaultTypeId = defaultType;
    update.mutate({ name, description, settings }, { onSuccess: () => setNotice(t.projects.saved) });
  }

  return (
    <Card className="space-y-4 p-4">
      <SectionTitle>{t.projects.detailsTitle}</SectionTitle>
      <form onSubmit={save} className="space-y-4" noValidate data-project-details>
        {update.error && !Object.keys(fields).length && <ErrorBanner>{update.error.message}</ErrorBanner>}
        <Field
          label={t.projects.name}
          value={name}
          maxLength={PROJECT_NAME_MAX_LENGTH}
          onChange={(event) => setName(event.target.value)}
          readOnly={readOnly}
          error={fields.name}
        />
        <Field
          label={t.projects.description}
          value={description}
          rows={3}
          maxLength={PROJECT_DESCRIPTION_MAX_LENGTH}
          onChange={(event) => setDescription(event.target.value)}
          readOnly={readOnly}
          error={fields.description}
        />
        <Select
          label={t.types.defaultType}
          hint={t.types.defaultTypeHint}
          value={defaultType}
          disabled={readOnly}
          onChange={(event) => setDefaultType(event.target.value)}
          error={fields["settings.defaultTypeId"]}
        >
          {(types ?? [])
            .filter((type) => !type.retired || type.id === defaultType)
            .map((type) => (
              <option key={type.id} value={type.id}>
                {type.name}
              </option>
            ))}
        </Select>
        <div>
          <Checkbox
            label={t.projects.requireComment}
            checked={requireComment}
            disabled={readOnly}
            onChange={(event) => setRequireComment(event.target.checked)}
            aria-describedby="require-comment-hint"
          />
          <p id="require-comment-hint" className="mt-1 text-xs text-ink-muted">
            {t.projects.requireCommentHint}
          </p>
        </div>
        {!readOnly && (
          <div className="flex items-center gap-3">
            <Button type="submit" loading={update.isPending} data-action="save-project">
              {t.projects.save}
            </Button>
            <span role="status" className="text-sm text-ink-muted">
              {notice}
            </span>
          </div>
        )}
      </form>
    </Card>
  );
}

function KeySection({ project, readOnly }: { project: Project; readOnly: boolean }) {
  const change = useChangeProjectKey(project.key);
  const navigate = useNavigate();
  const [next, setNext] = useState("");
  const [notice, setNotice] = useState("");
  const fields = change.error instanceof ApiError ? change.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    change.mutate(next, {
      onSuccess: (changed) => {
        setNext("");
        setNotice(t.projects.keyChanged(changed.key));
        void navigate({ to: "/p/$projectKey/settings", params: { projectKey: changed.key }, replace: true });
      },
    });
  }

  return (
    <Card className="space-y-3 p-4" data-project-key-section>
      <SectionTitle>{t.projects.keyTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.projects.keyIntro}</p>
      {project.formerKeys.length > 0 && (
        <p className="text-sm text-ink" data-former-keys>
          {t.projects.formerKeys(project.formerKeys.join(", "))}
        </p>
      )}
      {!readOnly && (
        <form onSubmit={submit} className="flex flex-wrap items-end gap-2" noValidate>
          <div className="w-48">
            <Field
              label={t.projects.newKey}
              value={next}
              maxLength={PROJECT_KEY_MAX_LENGTH}
              className="font-mono uppercase"
              onChange={(event) => setNext(event.target.value.toUpperCase())}
              error={fields.key}
            />
          </div>
          <Button type="submit" variant="secondary" loading={change.isPending} disabled={!next.trim()} data-action="change-key">
            {t.projects.changeKey}
          </Button>
        </form>
      )}
      {change.error && !Object.keys(fields).length && <ErrorBanner>{change.error.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted">
        {notice}
      </p>
    </Card>
  );
}

function ArchiveSection({ project }: { project: Project }) {
  const setArchived = useSetArchived(project.key);
  const archived = Boolean(project.archivedAt);
  return (
    <Card className="space-y-3 p-4" data-project-archive>
      <SectionTitle>{t.projects.archiveTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.projects.archiveBody}</p>
      {setArchived.error && <ErrorBanner>{setArchived.error.message}</ErrorBanner>}
      <Button
        variant="secondary"
        loading={setArchived.isPending}
        onClick={() => setArchived.mutate(!archived)}
        data-action={archived ? "unarchive-project" : "archive-project"}
      >
        {archived ? t.projects.unarchive : t.projects.archive}
      </Button>
    </Card>
  );
}

function DeleteSection({ project }: { project: Project }) {
  const remove = useDeleteProject(project.key);
  const navigate = useNavigate();
  const [confirming, setConfirming] = useState(false);
  return (
    <Card className="space-y-3 border-danger/40 p-4" data-project-danger>
      <SectionTitle>{t.projects.dangerTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.projects.dangerBody}</p>
      <Button variant="danger" onClick={() => setConfirming(true)} data-action="delete-project">
        {t.projects.delete}
      </Button>
      {confirming && (
        <Dialog title={t.projects.confirmTitle} onClose={() => setConfirming(false)} data-confirm-delete-project="">
          <p className="text-sm text-ink">{t.projects.confirmBody(project.name, project.key)}</p>
          {remove.error && (
            <div className="mt-3">
              <ErrorBanner>{remove.error.message}</ErrorBanner>
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setConfirming(false)}>
              {t.common.cancel}
            </Button>
            <Button
              variant="danger"
              loading={remove.isPending}
              onClick={() => remove.mutate(undefined, { onSuccess: () => void navigate({ to: PROJECTS_PATH }) })}
              data-action="confirm-delete-project"
            >
              {t.projects.confirmDelete}
            </Button>
          </div>
        </Dialog>
      )}
    </Card>
  );
}
