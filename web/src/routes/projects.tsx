import { useEffect, type ReactNode } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { useProject, type Project } from "@/api/projects";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { NEW_PROJECT_PATH, NEW_WORKFLOW, PROJECTS_PATH } from "@/config";
import { NewProjectForm } from "@/features/projects/NewProjectForm";
import { ProjectDirectory } from "@/features/projects/ProjectDirectory";
import { ProjectPermissions } from "@/features/permissions/ProjectPermissions";
import { ProjectSettings, isSettingsTab, type SettingsTab } from "@/features/projects/ProjectSettings";
import { NewRequirementForm } from "@/features/requirements/NewRequirementForm";
import { RequirementList } from "@/features/requirements/RequirementList";
import { TrashPage } from "@/features/requirements/TrashPage";
import { readAddress } from "@/features/grid/filter";
import { GridPage } from "@/features/grid/GridPage";
import { SpecPage } from "@/features/tree/SpecPage";
import { WorkflowDesignPage } from "@/features/workflows/WorkflowDesignPage";
import { appRoute } from "./app";
import { SuspectList } from "@/features/links/SuspectList";

export const projectsRoute = createRoute({ getParentRoute: () => appRoute, path: PROJECTS_PATH, component: ProjectDirectory });
export const newProjectRoute = createRoute({ getParentRoute: () => appRoute, path: NEW_PROJECT_PATH, component: NewProjectForm });

type ProjectPath =
  | "/p/$projectKey"
  | "/p/$projectKey/settings"
  | "/p/$projectKey/permissions"
  | "/p/$projectKey/new"
  | "/p/$projectKey/trash"
  | "/p/$projectKey/spec"
  | "/p/$projectKey/grid"
  | "/p/$projectKey/suspects";

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
  validateSearch: (search: Record<string, unknown>): { label?: string } => (typeof search.label === "string" && search.label ? { label: search.label } : {}),
  component: function ProjectHomePage() {
    const { projectKey } = projectRoute.useParams();
    const { label } = projectRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey"
        render={(project) => <RequirementList key={`${project.id}:${label ?? ""}`} project={project} label={label} />}
      />
    );
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

export const specRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/spec",
  component: function ProjectSpecPage() {
    const { projectKey } = specRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/spec" render={(project) => <SpecPage key={project.id} project={project} />} />;
  },
});

/** A project's grid, whose narrowing, order, columns and open view the address holds. */
export const gridRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/grid",
  validateSearch: readAddress,
  component: function ProjectGridPage() {
    const { projectKey } = gridRoute.useParams();
    const address = gridRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/grid"
        render={(project) => (
          <GridPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/p/$projectKey/grid", params: { projectKey: project.key }, search: next, replace: true })}
          />
        )}
      />
    );
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

/** The suspect links touching a project's requirements (#36). */
export const suspectsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/suspects",
  component: function ProjectSuspectsPage() {
    const { projectKey } = suspectsRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/suspects" render={(project) => <SuspectList key={project.id} project={project} />} />;
  },
});

export const projectSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/settings",
  validateSearch: (search: Record<string, unknown>): { tab?: SettingsTab } => (isSettingsTab(search.tab) ? { tab: search.tab } : {}),
  component: function ProjectSettingsPage() {
    const { projectKey } = projectSettingsRoute.useParams();
    const { tab } = projectSettingsRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/settings"
        render={(project) => <ProjectSettings key={project.id} project={project} tab={tab} />}
      />
    );
  },
});

/** The designer, wide, on one of a project's workflows or on a new one. */
export const workflowDesignRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/settings/workflows/$workflowId",
  component: function WorkflowDesignRoute() {
    const { projectKey, workflowId } = workflowDesignRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/settings"
        render={(project) => <WorkflowDesignPage key={project.id} project={project} workflowId={workflowId === NEW_WORKFLOW ? undefined : workflowId} />}
      />
    );
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
