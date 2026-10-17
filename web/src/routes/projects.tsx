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
import { readSpecAddress } from "@/features/tree/address";
import { SpecPage } from "@/features/tree/SpecPage";
import { WorkflowDesignPage } from "@/features/workflows/WorkflowDesignPage";
import { appRoute } from "./app";
import { SuspectList } from "@/features/links/SuspectList";
import { BaselineList } from "@/features/baselines/Baselines";
import { BaselinePage } from "@/features/baselines/BaselinePage";
import { BaselineCompare } from "@/features/baselines/BaselineCompare";
import { SessionList } from "@/features/sessions/SessionList";
import { SessionPage } from "@/features/sessions/SessionPage";
import { CoveragePage } from "@/features/coverage/CoveragePage";
import { readMatrixAddress } from "@/features/matrix/address";
import { readChainsAddress } from "@/features/matrix/chainsAddress";
import { ChainsPage } from "@/features/matrix/ChainsPage";
import { MatrixPage } from "@/features/matrix/MatrixPage";

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
  | "/p/$projectKey/suspects"
  | "/p/$projectKey/baselines"
  | "/p/$projectKey/sessions"
  | "/p/$projectKey/coverage"
  | "/p/$projectKey/matrix"
  | "/p/$projectKey/chains";

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
  validateSearch: readSpecAddress,
  component: function ProjectSpecPage() {
    const { projectKey } = specRoute.useParams();
    const address = specRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/spec"
        render={(project) => (
          <SpecPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/p/$projectKey/spec", params: { projectKey }, search: next })}
          />
        )}
      />
    );
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

/** A project's review sessions (#204). */
export const sessionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/sessions",
  component: function ProjectSessionsPage() {
    const { projectKey } = sessionsRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/sessions" render={(project) => <SessionList key={project.id} project={project} />} />;
  },
});

export const sessionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/sessions/$sessionId",
  component: function ProjectSessionPage() {
    const { projectKey, sessionId } = sessionRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/sessions"
        render={(project) => <SessionPage key={sessionId} project={project} sessionId={sessionId} />}
      />
    );
  },
});

/** A project's baselines (#37). */
export const baselinesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/baselines",
  component: function ProjectBaselinesPage() {
    const { projectKey } = baselinesRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/baselines" render={(project) => <BaselineList key={project.id} project={project} />} />;
  },
});

/** A project's coverage (#38), which leads to its traceability matrix. */
export const coverageRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/coverage",
  component: function ProjectCoveragePage() {
    const { projectKey } = coverageRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/p/$projectKey/coverage" render={(project) => <CoveragePage key={project.id} project={project} />} />;
  },
});

/** A project's traceability matrix (#147), whose rows, columns and counted links the address holds. */
export const matrixRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/matrix",
  validateSearch: readMatrixAddress,
  component: function ProjectMatrixPage() {
    const { projectKey } = matrixRoute.useParams();
    const address = matrixRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/matrix"
        render={(project) => (
          <MatrixPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/p/$projectKey/matrix", params: { projectKey: project.key }, search: next, replace: true })}
          />
        )}
      />
    );
  },
});

/** A project's chains (#151), whose roots, levels and breaks the address holds. */
export const chainsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/chains",
  validateSearch: readChainsAddress,
  component: function ProjectChainsPage() {
    const { projectKey } = chainsRoute.useParams();
    const address = chainsRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/chains"
        render={(project) => (
          <ChainsPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/p/$projectKey/chains", params: { projectKey: project.key }, search: next, replace: true })}
          />
        )}
      />
    );
  },
});

/** One baseline, read as a specification. */
export const baselineRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/baselines/$baselineId",
  component: function ProjectBaselinePage() {
    const { projectKey, baselineId } = baselineRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/baselines"
        render={(project) => <BaselinePage key={baselineId} project={project} baselineId={baselineId} />}
      />
    );
  },
});

/** A baseline compared with another of its project, or with now. */
export const baselineCompareRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/baselines/$baselineId/compare",
  validateSearch: (search: Record<string, unknown>): { with?: string } => (typeof search.with === "string" && search.with ? { with: search.with } : {}),
  component: function ProjectBaselineComparePage() {
    const { projectKey, baselineId } = baselineCompareRoute.useParams();
    const { with: other } = baselineCompareRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/p/$projectKey/baselines"
        render={(project) => <BaselineCompare project={project} baselineId={baselineId} other={other ?? ""} />}
      />
    );
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
