import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { administers, useMe } from "@/api/auth";
import { useProjects } from "@/api/projects";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Switch, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { NEW_PROJECT_PATH } from "@/config";
import { t } from "@/i18n";
import { ProjectTable } from "./ProjectTable";

/** Every project the reader may view, archived ones when asked, and for administrators the way to make another. */
export function ProjectDirectory() {
  const [showArchived, setShowArchived] = useState(false);
  const { data: projects, isLoading, error, refetch } = useProjects(showArchived);
  const { data: me } = useMe();
  const mayCreate = administers(me?.role);
  const navigate = useNavigate();
  const create = (action: string) =>
    mayCreate ? (
      <Button icon={<Icon.Plus />} onClick={() => navigate({ to: NEW_PROJECT_PATH })} data-action={action}>
        {t.projects.create}
      </Button>
    ) : undefined;

  return (
    <Page width="content" data-project-directory>
      <PageHeader
        title={t.projects.title}
        actions={
          <>
            <span className="inline-flex items-center gap-2 text-sm text-ink-muted">
              <Switch label={t.projects.showArchived} checked={showArchived} onChange={setShowArchived} data-show-archived="" />
              <span aria-hidden="true">{t.projects.showArchived}</span>
            </span>
            {create("new-project")}
          </>
        }
      />
      <p className="mb-4 text-sm text-ink-muted">{t.projects.intro}</p>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {projects && projects.length === 0 && (
        <EmptyState
          icon={<Icon.Folder />}
          title={t.projects.emptyTitle}
          description={mayCreate ? t.projects.emptyBody : t.projects.emptyBodyMember}
          action={create("first-project")}
        />
      )}
      {projects && projects.length > 0 && <ProjectTable projects={projects} label={t.projects.title} />}
    </Page>
  );
}
