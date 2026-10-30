import { useNavigate } from "@tanstack/react-router";
import { administers, useMe } from "@/api/auth";
import { useProjects } from "@/api/projects";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { NEW_PROJECT_PATH } from "@/config";
import { ProjectTable } from "@/features/projects/ProjectTable";
import { t } from "@/i18n";
import { MyExecutions } from "@/features/tests/MyExecutions";

/** Where the reader lands: their projects, or what Exciter is for and the way to the first project. */
export function HomeScreen() {
  const { data: me } = useMe();
  const { data: projects, isLoading, error, refetch } = useProjects();
  const navigate = useNavigate();
  const mayCreate = administers(me?.role);
  return (
    <Page width="narrow" data-home>
      <PageHeader title={t.home.title} meta={me?.installation.name} />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {projects && projects.length === 0 && (
        <EmptyState
          icon={<Icon.Home />}
          title={t.home.emptyTitle}
          description={mayCreate ? t.home.emptyBody : t.home.emptyBodyMember}
          action={
            mayCreate && (
              <Button icon={<Icon.Plus />} onClick={() => navigate({ to: NEW_PROJECT_PATH })} data-action="first-project">
                {t.projects.create}
              </Button>
            )
          }
        />
      )}
      <MyExecutions />
      {projects && projects.length > 0 && (
        <section aria-labelledby="home-projects">
          <h2 id="home-projects" className="mb-2 text-base font-semibold text-ink">
            {t.home.yourProjects}
          </h2>
          <ProjectTable projects={projects} label={t.home.yourProjects} />
        </section>
      )}
    </Page>
  );
}
