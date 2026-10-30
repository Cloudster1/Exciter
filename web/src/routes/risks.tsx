import { createRoute } from "@tanstack/react-router";
import { RisksPage, type RisksView } from "@/features/risks/RisksPage";
import { appRoute } from "./app";
import { ProjectPage } from "./projects";

// A project's risks (#260): the FMEA table and the risk matrix.

export const risksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/risks",
  validateSearch: (search: Record<string, unknown>): { view?: RisksView } => (search.view === "matrix" ? { view: "matrix" } : {}),
  component: function ProjectRisksPage() {
    const { projectKey } = risksRoute.useParams();
    const { view } = risksRoute.useSearch();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/risks"
        render={(project) => <RisksPage key={project.id} project={project} view={view ?? "table"} />}
      />
    );
  },
});
