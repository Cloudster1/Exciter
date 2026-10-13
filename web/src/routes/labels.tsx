import { createRoute, useNavigate } from "@tanstack/react-router";
import { LabelScreen } from "@/features/labels/LabelScreen";
import { appRoute } from "./app";

export interface LabelSearch {
  project?: string;
  page?: number;
}

/** A label's own page: the requirements that carry it, across projects or in one. */
export const labelRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/labels/$name",
  validateSearch: (search: Record<string, unknown>): LabelSearch => {
    const page = Number(search.page);
    return {
      project: typeof search.project === "string" && search.project ? search.project : undefined,
      page: Number.isInteger(page) && page > 1 ? page : undefined,
    };
  },
  component: function LabelPage() {
    const { name } = labelRoute.useParams();
    const { project, page } = labelRoute.useSearch();
    const navigate = useNavigate();
    return (
      <LabelScreen
        key={`${name}:${project ?? ""}`}
        name={name}
        projectKey={project}
        page={page ?? 1}
        onPage={(next) => void navigate({ to: "/labels/$name", params: { name }, search: { project, page: next > 1 ? next : undefined } })}
      />
    );
  },
});
