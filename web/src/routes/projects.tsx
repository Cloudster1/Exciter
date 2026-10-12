import { useEffect, type ReactNode } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { useProject, type Project } from "@/api/projects";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { NEW_PROJECT_PATH, PROJECTS_PATH } from "@/config";
import { NewProjectForm } from "@/features/projects/NewProjectForm";
import { ProjectDirectory } from "@/features/projects/ProjectDirectory";
import { ProjectPermissions } from "@/features/permissions/ProjectPermissions";
import { ProjectSettings } from "@/features/projects/ProjectSettings";
import { NewRequirementForm } from "@/features/requirements/NewRequirementForm";
import { RequirementList } from "@/features/requirements/RequirementList";
import { TrashPage } from "@/features/requirements/TrashPage";
import { appRoute } from "./app";

export const projectsRoute = createRoute({ getParentRoute: () => appRoute, path: PROJECTS_PATH, component: ProjectDirectory });
export const newProjectRoute = createRoute({ getParentRoute: () => appRoute, path: NEW_PROJECT_PATH, component: NewProjectForm });

type ProjectPath = "/p/$projectKey" | "/p/$projectKey/settings" | "/p/$projectKey/permissions" | "/p/$projectKey/new" | "/p/$projectKey/trash";

/** Reads the project a page is about; a key it had moves the address to the key it has now. */
export function ProjectPage({ projectKey, to, render }: { projectKey: string; to: ProjectPath; render: (project: Project) => ReactNode }) {
  const { data: project, error, refetch } = useProject(projectKey);
  const navigate = useNavigate();
  useEffect(() => {
    if (project && project.key !== projectKey) {
      void navigate({ to, params: { projectKey: project.key }, replace: true });
    }
  }, [project, projectKey, navigate, to]);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!project) return <Skeleton />;
  return render(project);
}

/** A project's own page: its requirements. */
export const projectRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey",
  component: function ProjectHomePage() {
    const { projectKey } = projectRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey" render={(project) => <RequirementList key={project.id} project={project} />} />;
  },
});

export const newRequirementRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/new",
  component: function NewRequirementPage() {
    const { projectKey } = newRequirementRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/new" render={(project) => <NewRequirementForm key={project.id} project={project} />} />;
  },
});

export const trashRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/trash",
  component: function ProjectTrashPage() {
    const { projectKey } = trashRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/trash" render={(project) => <TrashPage key={project.id} project={project} />} />;
  },
});

export const projectSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/settings",
  component: function ProjectSettingsPage() {
    const { projectKey } = projectSettingsRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/settings" render={(project) => <ProjectSettings key={project.id} project={project} />} />;
  },
});

export const projectPermissionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/permissions",
  component: function ProjectPermissionsPage() {
    const { projectKey } = projectPermissionsRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/p/$projectKey/permissions" render={(project) => <ProjectPermissions key={project.id} project={project} />} />
    );
  },
});
