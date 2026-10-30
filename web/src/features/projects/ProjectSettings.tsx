import { useId, useState, type FormEvent, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useDashboards } from "@/api/dashboards";
import { useTypes } from "@/api/attributes";
import { useChangeProjectKey, useDeleteProject, useSetArchived, useUpdateProject, type Project, type ProjectChanges } from "@/api/projects";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, Field, Page, PageHeader, SectionTitle, Select, TabPanel, Tabs, Tag, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { adminOnly, projectSettingsPath, sectionLabel, sectionsFor, type ProjectSettingsSection } from "./settingsSections";
import { ApprovalRules } from "@/features/reviews/ApprovalRules";
import { SigningRuleCard } from "@/features/baselines/SigningRule";
import { PROJECTS_PATH, PROJECT_DESCRIPTION_MAX_LENGTH, PROJECT_KEY_MAX_LENGTH, PROJECT_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { TraceModelCard } from "@/features/matrix/TraceModelCard";
import { EvidenceSettingsCard } from "@/features/evidence/EvidenceSettingsCard";
import { CodeTagWordsCard } from "@/features/evidence/CodeTagWordsCard";
import { QualityRulesCard, QualityWordsCard } from "@/features/quality/QualitySettingsCards";
import { GlossariesCard } from "@/features/glossary/GlossariesCard";
import { DuplicateSettingsCard } from "@/features/duplicates/DuplicateSettingsCard";

/**
 * A project's settings: the tabs of its sections in the header and the one
 * section shown. Every member opens them to see how the project is set up; what
 * only its administrators use is neither listed nor mounted for anybody else.
 */
export function ProjectSettingsLayout({ project, section, children }: { project: Project; section: ProjectSettingsSection; children: ReactNode }) {
  const navigate = useNavigate();
  const panelId = useId();
  const administers = project.can.administer;
  const refused = adminOnly(section) && !administers;
  return (
    <Page width="content" data-project-settings={project.key}>
      <PageHeader
        title={t.projects.settingsTitle}
        meta={<Tag className="font-mono">{project.key}</Tag>}
        tabs={
          <Tabs<ProjectSettingsSection>
            label={t.projects.settingsTabs}
            value={section}
            onChange={(next) => void navigate({ href: projectSettingsPath(project.key, next) })}
            panelId={panelId}
            tabs={sectionsFor(project).map((each) => ({ value: each, label: sectionLabel(each), attrs: { "data-settings-tab": each } }))}
          />
        }
      />
      {project.archivedAt && (
        <p role="status" className="mb-4 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-archived-notice>
          {t.projects.archivedNotice}
        </p>
      )}
      {!administers && !refused && (
        <p role="status" className="mb-4 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink-muted" data-settings-read-only>
          {t.projects.notProjectAdmin}
        </p>
      )}
      <TabPanel id={panelId} label={sectionLabel(section)}>
        {refused ? (
          <div data-settings-refused={section}>
            <EmptyState icon={<Icon.Lock />} title={t.projects.notAdminTitle} description={t.projects.notAdminDetail} />
          </div>
        ) : (
          <div className="space-y-6" data-settings-section={section}>
            {children}
          </div>
        )}
      </TabPanel>
    </Page>
  );
}

/** The project's details, and at the foot its key, archiving and deleting it. */
export function DetailsSection({ project }: { project: Project }) {
  const readOnly = !project.can.administer;
  return (
    <>
      <Details key={`${project.id}-${project.updatedAt}`} project={project} readOnly={readOnly || Boolean(project.archivedAt)} />
      <KeySection project={project} readOnly={readOnly || Boolean(project.archivedAt)} />
      {!readOnly && <ArchiveSection project={project} />}
      {!readOnly && project.archivedAt && <DeleteSection project={project} />}
    </>
  );
}

export function ApprovalSection({ project }: { project: Project }) {
  return (
    <>
      <ApprovalRules project={project} />
      <SigningRuleCard project={project} />
    </>
  );
}

export function TraceSection({ project }: { project: Project }) {
  return (
    <>
      <TraceModelCard project={project} />
      <EvidenceSettingsCard project={project} />
      <CodeTagWordsCard project={project} />
    </>
  );
}

export function QualitySection({ project }: { project: Project }) {
  return (
    <>
      <QualityRulesCard project={project} />
      <QualityWordsCard project={project} />
      <GlossariesCard project={project} />
      <DuplicateSettingsCard project={project} />
    </>
  );
}

function Details({ project, readOnly }: { project: Project; readOnly: boolean }) {
  const update = useUpdateProject(project.key);
  const toast = useToast();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [requireComment, setRequireComment] = useState(project.settings.requireRevisionComment);
  const [signedApprovals, setSignedApprovals] = useState(project.settings.signedApprovals);
  const [library, setLibrary] = useState(project.settings.library);
  const [defaultType, setDefaultType] = useState(project.settings.defaultTypeId ?? "");
  const [language, setLanguage] = useState(project.settings.language === "de" ? "de" : "en");
  const [home, setHome] = useState(project.settings.homeDashboardId ?? "");
  const { data: dashboards } = useDashboards(project.key);
  const { data: types } = useTypes(project.key);
  const fields = update.error instanceof ApiError ? update.error.fields : {};

  function save(event: FormEvent) {
    event.preventDefault();
    const settings: ProjectChanges["settings"] = { requireRevisionComment: requireComment, signedApprovals, library };
    if (defaultType && defaultType !== project.settings.defaultTypeId) settings.defaultTypeId = defaultType;
    // The language is sent only when it changes, since a change checks every requirement again (#207).
    if (language !== (project.settings.language ?? "en")) settings.language = language;
    if (home !== (project.settings.homeDashboardId ?? "")) settings.homeDashboardId = home || null;
    // This form is gone by the time mutate's own callbacks would run, so the promise says it saved; a refusal shows
    // through update.error.
    update.mutateAsync({ name, description, settings }).then(
      () => toast.success(t.projects.saved),
      () => undefined,
    );
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
        <Select
          label={t.quality.language.label}
          hint={t.quality.language.hint}
          value={language}
          disabled={readOnly}
          onChange={(event) => setLanguage(event.target.value === "de" ? "de" : "en")}
          error={fields["settings.language"] ?? fields.language}
          data-setting="language"
        >
          {(["en", "de"] as const).map((each) => (
            <option key={each} value={each} lang={each}>
              {t.quality.language.names[each]}
            </option>
          ))}
        </Select>
        <Select
          label={t.dashboard.home}
          hint={t.dashboard.homeHint}
          value={home}
          disabled={readOnly}
          onChange={(event) => setHome(event.target.value)}
          error={fields["settings.homeDashboardId"]}
          data-setting="home-dashboard"
        >
          <option value="">{t.dashboard.noHome}</option>
          {(dashboards ?? [])
            .filter((d) => d.shared)
            .map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
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
        <div>
          <Checkbox
            label={t.projects.signedApprovals}
            checked={signedApprovals}
            disabled={readOnly}
            onChange={(event) => setSignedApprovals(event.target.checked)}
            aria-describedby="signed-approvals-hint"
            data-setting="signed-approvals"
          />
          <p id="signed-approvals-hint" className="mt-1 text-xs text-ink-muted">
            {t.projects.signedApprovalsHint}
          </p>
        </div>
        <div>
          <Checkbox
            label={t.projects.library}
            checked={library}
            disabled={readOnly}
            onChange={(event) => setLibrary(event.target.checked)}
            aria-describedby="library-hint"
            data-setting="library"
          />
          <p id="library-hint" className="mt-1 text-xs text-ink-muted">
            {t.projects.libraryHint}
          </p>
        </div>
        {!readOnly && (
          <div className="flex items-center gap-3">
            <Button type="submit" loading={update.isPending} data-action="save-project">
              {t.projects.save}
            </Button>
          </div>
        )}
      </form>
    </Card>
  );
}

function KeySection({ project, readOnly }: { project: Project; readOnly: boolean }) {
  const change = useChangeProjectKey(project.key);
  const navigate = useNavigate();
  const toast = useToast();
  const [next, setNext] = useState("");
  const fields = change.error instanceof ApiError ? change.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    change.mutate(next, {
      onSuccess: (changed) => {
        setNext("");
        toast.success(t.projects.keyChanged(changed.key));
        void navigate({ href: projectSettingsPath(changed.key), replace: true });
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
    </Card>
  );
}

function ArchiveSection({ project }: { project: Project }) {
  const setArchived = useSetArchived(project.key);
  const toast = useToast();
  const archived = Boolean(project.archivedAt);
  const unarchive = () => setArchived.mutate(false, { onSuccess: () => toast.success(t.projects.unarchivedToast(project.name)) });
  return (
    <Card className="space-y-3 p-4" data-project-archive>
      <SectionTitle>{t.projects.archiveTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.projects.archiveBody}</p>
      {setArchived.error && <ErrorBanner>{setArchived.error.message}</ErrorBanner>}
      <Button
        variant="secondary"
        loading={setArchived.isPending}
        onClick={() => {
          if (archived) {
            unarchive();
            return;
          }
          setArchived.mutate(true, {
            onSuccess: () => toast.success(t.projects.archivedToast(project.name), { action: { label: t.projects.undo, onClick: unarchive } }),
          });
        }}
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
  const confirm = useConfirm();
  return (
    <Card className="space-y-3 border-danger/40 p-4" data-project-danger>
      <SectionTitle>{t.projects.dangerTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.projects.dangerBody}</p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      <Button
        variant="danger"
        loading={remove.isPending}
        onClick={async () => {
          if (await confirm({ noun: t.kit.confirm.things.project(project.name), body: t.projects.confirmBody(project.name, project.key) })) {
            remove.mutate(undefined, { onSuccess: () => void navigate({ to: PROJECTS_PATH }) });
          }
        }}
        data-action="delete-project"
      >
        {t.projects.delete}
      </Button>
    </Card>
  );
}
