import { useEffect, type ReactNode } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { useProject, type Project } from "@/api/projects";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { NEW_PROJECT_PATH, PROJECTS_PATH } from "@/config";
import type { ProjectSettingsSection } from "@/features/projects/settingsSections";
import { DashboardPage, DashboardsPage, HomeDashboard } from "@/features/dashboard/DashboardPages";
import { NewProjectForm } from "@/features/projects/NewProjectForm";
import { ProjectDirectory } from "@/features/projects/ProjectDirectory";
import { NewRequirementForm } from "@/features/requirements/NewRequirementForm";
import { RequirementList } from "@/features/requirements/RequirementList";
import { readAddress } from "@/features/grid/filter";
import { GridPage } from "@/features/grid/GridPage";
import { readSpecAddress } from "@/features/tree/address";
import { SpecPage } from "@/features/tree/SpecPage";
import { appRoute } from "./app";
import { SuspectList } from "@/features/links/SuspectList";
import { BaselineList } from "@/features/baselines/Baselines";
import { BaselinePage } from "@/features/baselines/BaselinePage";
import { BaselineCompare } from "@/features/baselines/BaselineCompare";
import { SessionList } from "@/features/sessions/SessionList";
import { SessionPage } from "@/features/sessions/SessionPage";
import { CoveragePage } from "@/features/coverage/CoveragePage";
import { CodeTagsPage } from "@/features/evidence/CodeTagsPage";
import { QualityPage } from "@/features/quality/QualityPage";
import { GlossaryPage } from "@/features/glossary/GlossaryPage";
import { TermPage } from "@/features/glossary/TermPage";
import { DuplicatesPage } from "@/features/duplicates/DuplicatesPage";
import { readMatrixAddress } from "@/features/matrix/address";
import { readChainsAddress } from "@/features/matrix/chainsAddress";
import { ChainsPage } from "@/features/matrix/ChainsPage";
import { MatrixPage } from "@/features/matrix/MatrixPage";

export const projectsRoute = createRoute({ getParentRoute: () => appRoute, path: PROJECTS_PATH, component: ProjectDirectory });
// A project's key is upper case and may be NEW, so this page matches only as written, and /projects/NEW is that project.
export const newProjectRoute = createRoute({ getParentRoute: () => appRoute, path: NEW_PROJECT_PATH, caseSensitive: true, component: NewProjectForm });

type ProjectPath =
  | "/projects/$projectKey"
  | `/projects/$projectKey/settings/${ProjectSettingsSection}`
  | "/projects/$projectKey/new"
  | "/projects/$projectKey/dashboards"
  | "/projects/$projectKey/spec"
  | "/projects/$projectKey/grid"
  | "/projects/$projectKey/suspects"
  | "/projects/$projectKey/baselines"
  | "/projects/$projectKey/sessions"
  | "/projects/$projectKey/tests"
  | "/projects/$projectKey/risks"
  | "/projects/$projectKey/coverage"
  | "/projects/$projectKey/code-tags"
  | "/projects/$projectKey/quality"
  | "/projects/$projectKey/glossary"
  | "/projects/$projectKey/duplicates"
  | "/projects/$projectKey/matrix"
  | "/projects/$projectKey/chains";

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
  path: "/projects/$projectKey",
  validateSearch: (search: Record<string, unknown>): { label?: string } => (typeof search.label === "string" && search.label ? { label: search.label } : {}),
  component: function ProjectHomePage() {
    const { projectKey } = projectRoute.useParams();
    const { label } = projectRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey"
        render={(project) => (
          <>
            <HomeDashboard project={project} />
            <RequirementList key={`${project.id}:${label ?? ""}`} project={project} label={label} />
          </>
        )}
      />
    );
  },
});

export const newRequirementRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/new",
  component: function NewRequirementPage() {
    const { projectKey } = newRequirementRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/new" render={(project) => <NewRequirementForm key={project.id} project={project} />} />
    );
  },
});

export const specRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/spec",
  validateSearch: readSpecAddress,
  component: function ProjectSpecPage() {
    const { projectKey } = specRoute.useParams();
    const address = specRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/spec"
        render={(project) => (
          <SpecPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/projects/$projectKey/spec", params: { projectKey }, search: next })}
          />
        )}
      />
    );
  },
});

/** A project's grid, whose narrowing, order, columns and open view the address holds. */
export const gridRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/grid",
  validateSearch: readAddress,
  component: function ProjectGridPage() {
    const { projectKey } = gridRoute.useParams();
    const address = gridRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/grid"
        render={(project) => (
          <GridPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/projects/$projectKey/grid", params: { projectKey: project.key }, search: next, replace: true })}
          />
        )}
      />
    );
  },
});

/** The suspect links touching a project's requirements (#36). */
export const suspectsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/suspects",
  component: function ProjectSuspectsPage() {
    const { projectKey } = suspectsRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/projects/$projectKey/suspects" render={(project) => <SuspectList key={project.id} project={project} />} />;
  },
});

/** A project's review sessions (#204). */
export const sessionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/sessions",
  component: function ProjectSessionsPage() {
    const { projectKey } = sessionsRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/projects/$projectKey/sessions" render={(project) => <SessionList key={project.id} project={project} />} />;
  },
});

export const sessionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/sessions/$sessionId",
  component: function ProjectSessionPage() {
    const { projectKey, sessionId } = sessionRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/sessions"
        render={(project) => <SessionPage key={sessionId} project={project} sessionId={sessionId} />}
      />
    );
  },
});

/** A project's baselines (#37). */
export const baselinesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/baselines",
  component: function ProjectBaselinesPage() {
    const { projectKey } = baselinesRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/baselines" render={(project) => <BaselineList key={project.id} project={project} />} />
    );
  },
});

/** A project's coverage (#38), which leads to its traceability matrix. */
export const coverageRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/coverage",
  component: function ProjectCoveragePage() {
    const { projectKey } = coverageRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/coverage" render={(project) => <CoveragePage key={project.id} project={project} />} />
    );
  },
});

/** A project's outdated and orphaned code tags (#201), where a scan's notification leads. */
export const codeTagsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/code-tags",
  component: function ProjectCodeTagsPage() {
    const { projectKey } = codeTagsRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/code-tags" render={(project) => <CodeTagsPage key={project.id} project={project} />} />
    );
  },
});

/** A project's dashboards (#212). */
export const dashboardsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/dashboards",
  component: function ProjectDashboardsPage() {
    const { projectKey } = dashboardsRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/dashboards" render={(project) => <DashboardsPage key={project.id} project={project} />} />
    );
  },
});

/** One dashboard; its address is the link a shared one is passed on by. */
export const dashboardRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/dashboards/$dashboardId",
  component: function ProjectDashboardPage() {
    const { projectKey, dashboardId } = dashboardRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/dashboards"
        render={(project) => <DashboardPage key={dashboardId} project={project} dashboardId={dashboardId} />}
      />
    );
  },
});

/** How a project's requirements keep to the writing rules (#207). */
export const qualityRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/quality",
  component: function ProjectQualityPage() {
    const { projectKey } = qualityRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/projects/$projectKey/quality" render={(project) => <QualityPage key={project.id} project={project} />} />;
  },
});

/** A project's glossary (#210): its terms and its libraries'. */
export const glossaryRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/glossary",
  component: function ProjectGlossaryPage() {
    const { projectKey } = glossaryRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/glossary" render={(project) => <GlossaryPage key={project.id} project={project} />} />
    );
  },
});

/** A project's likely duplicates (#208), to dismiss, link or merge. */
export const duplicatesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/duplicates",
  component: function ProjectDuplicatesPage() {
    const { projectKey } = duplicatesRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/duplicates" render={(project) => <DuplicatesPage key={project.id} project={project} />} />
    );
  },
});

/** One term of a glossary, with its revisions and the requirements that use it. */
export const termRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/glossary/$termId",
  component: function ProjectTermPage() {
    const { projectKey, termId } = termRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/glossary"
        render={(project) => <TermPage key={termId} project={project} termId={termId} />}
      />
    );
  },
});

/** A project's traceability matrix (#147), whose rows, columns and counted links the address holds. */
export const matrixRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/matrix",
  validateSearch: readMatrixAddress,
  component: function ProjectMatrixPage() {
    const { projectKey } = matrixRoute.useParams();
    const address = matrixRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/matrix"
        render={(project) => (
          <MatrixPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/projects/$projectKey/matrix", params: { projectKey: project.key }, search: next, replace: true })}
          />
        )}
      />
    );
  },
});

/** A project's chains (#151), whose roots, levels and breaks the address holds. */
export const chainsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/chains",
  validateSearch: readChainsAddress,
  component: function ProjectChainsPage() {
    const { projectKey } = chainsRoute.useParams();
    const address = chainsRoute.useSearch();
    const navigate = useNavigate();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/chains"
        render={(project) => (
          <ChainsPage
            key={project.id}
            project={project}
            address={address}
            onAddress={(next) => void navigate({ to: "/projects/$projectKey/chains", params: { projectKey: project.key }, search: next, replace: true })}
          />
        )}
      />
    );
  },
});

/** One baseline, read as a specification. */
export const baselineRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/baselines/$baselineId",
  component: function ProjectBaselinePage() {
    const { projectKey, baselineId } = baselineRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/baselines"
        render={(project) => <BaselinePage key={baselineId} project={project} baselineId={baselineId} />}
      />
    );
  },
});

/** A baseline compared with another of its project, or with now. */
export const baselineCompareRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/baselines/$baselineId/compare",
  validateSearch: (search: Record<string, unknown>): { with?: string } => (typeof search.with === "string" && search.with ? { with: search.with } : {}),
  component: function ProjectBaselineComparePage() {
    const { projectKey, baselineId } = baselineCompareRoute.useParams();
    const { with: other } = baselineCompareRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/baselines"
        render={(project) => <BaselineCompare project={project} baselineId={baselineId} other={other ?? ""} />}
      />
    );
  },
});
