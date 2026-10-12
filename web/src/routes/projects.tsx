import { useEffect, type ReactNode } from "react";
import { createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useProject, type Project } from "@/api/projects";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { NEW_PROJECT_PATH, PROJECTS_PATH } from "@/config";
import { NewProjectForm } from "@/features/projects/NewProjectForm";
import { ProjectDirectory } from "@/features/projects/ProjectDirectory";
import { ProjectPermissions } from "@/features/permissions/ProjectPermissions";
import { ProjectSettings } from "@/features/projects/ProjectSettings";
import { appRoute } from "./app";

export const projectsRoute = createRoute({ getParentRoute: () => appRoute, path: PROJECTS_PATH, component: ProjectDirectory });
export const newProjectRoute = createRoute({ getParentRoute: () => appRoute, path: NEW_PROJECT_PATH, component: NewProjectForm });

/** A project's own page; until requirements arrive (#13) its settings are all there is to show. */
export const projectRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/p/$projectKey/settings", params, replace: true });
  },
});

/** Reads the project a page is about; a key it had moves the address to the key it has now. */
function ProjectPage({
  projectKey,
  to,
  render,
}: {
  projectKey: string;
  to: "/p/$projectKey/settings" | "/p/$projectKey/permissions";
  render: (project: Project) => ReactNode;
}) {
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
