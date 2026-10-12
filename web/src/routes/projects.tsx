import { useEffect } from "react";
import { createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useProject } from "@/api/projects";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { NEW_PROJECT_PATH, PROJECTS_PATH } from "@/config";
import { NewProjectForm } from "@/features/projects/NewProjectForm";
import { ProjectDirectory } from "@/features/projects/ProjectDirectory";
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

export const projectSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/settings",
  component: function ProjectSettingsPage() {
    const { projectKey } = projectSettingsRoute.useParams();
    const { data: project, error, refetch } = useProject(projectKey);
    const navigate = useNavigate();
    // A key the project had still finds it; the address then names the key it has now.
    useEffect(() => {
      if (project && project.key !== projectKey) {
        void navigate({ to: "/p/$projectKey/settings", params: { projectKey: project.key }, replace: true });
      }
    }, [project, projectKey, navigate]);
    if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
    if (!project) return <Skeleton />;
    return <ProjectSettings key={project.id} project={project} />;
  },
});
