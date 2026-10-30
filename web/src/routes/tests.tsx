import { createRoute } from "@tanstack/react-router";
import { ExecutionPage } from "@/features/tests/ExecutionPage";
import { PlanPage } from "@/features/tests/PlanPage";
import { StepRunner } from "@/features/tests/StepRunner";
import { TestsPage } from "@/features/tests/TestsPage";
import { appRoute } from "./app";
import { ProjectPage } from "./projects";

// A project's tests (#251): its plans and executions, and the step runner.

export const testsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/tests",
  component: function ProjectTestsPage() {
    const { projectKey } = testsRoute.useParams();
    return <ProjectPage projectKey={projectKey} to="/projects/$projectKey/tests" render={(project) => <TestsPage key={project.id} project={project} />} />;
  },
});

export const testPlanRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/tests/plans/$planId",
  component: function ProjectTestPlanPage() {
    const { projectKey, planId } = testPlanRoute.useParams();
    return (
      <ProjectPage projectKey={projectKey} to="/projects/$projectKey/tests" render={(project) => <PlanPage key={planId} project={project} planId={planId} />} />
    );
  },
});

export const testExecutionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/tests/executions/$executionId",
  component: function ProjectTestExecutionPage() {
    const { projectKey, executionId } = testExecutionRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/tests"
        render={(project) => <ExecutionPage key={executionId} project={project} executionId={executionId} />}
      />
    );
  },
});

export const testRunRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/tests/executions/$executionId/run/$reqKey",
  validateSearch: (search: Record<string, unknown>): { iteration?: number } => {
    const n = Number(search.iteration);
    return Number.isInteger(n) && n >= 1 ? { iteration: n } : {};
  },
  component: function ProjectTestRunPage() {
    const { projectKey, executionId, reqKey } = testRunRoute.useParams();
    const { iteration } = testRunRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/tests"
        render={(project) => <StepRunner project={project} executionId={executionId} caseKey={reqKey} iteration={iteration ?? 1} />}
      />
    );
  },
});
