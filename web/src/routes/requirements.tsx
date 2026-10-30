import { useEffect } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { useProject } from "@/api/projects";
import { useRequirement, useRequirementById } from "@/api/requirements";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { RequirementHistory, type HistorySearch } from "@/features/history/RequirementHistory";
import { RequirementPage } from "@/features/requirements/RequirementPage";
import { readGraphAddress, type GraphAddress } from "@/features/trace/address";
import { TraceGraphPage } from "@/features/trace/TraceGraphPage";
import { appRoute } from "./app";

/** A requirement's page. A key it had, or its project had, still finds it, and the address follows to the current one. */
export const requirementRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/r/$reqKey",
  component: function RequirementRoutePage() {
    const { reqKey } = requirementRoute.useParams();
    const { data: requirement, error, refetch } = useRequirement(reqKey);
    const { data: project, error: projectError } = useProject(requirement?.projectKey ?? "");
    const navigate = useNavigate();
    useEffect(() => {
      if (requirement && requirement.key !== reqKey) {
        void navigate({
          to: "/projects/$projectKey/r/$reqKey",
          params: { projectKey: requirement.projectKey, reqKey: requirement.key },
          hash: true,
          replace: true,
        });
      }
    }, [requirement, reqKey, navigate]);
    if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
    if (projectError) return <ErrorBanner>{projectError.message}</ErrorBanner>;
    if (!requirement || !project) return <Skeleton />;
    return <RequirementPage key={requirement.id} project={project} requirement={requirement} />;
  },
});

/** A permalink: the id never changes, and the address becomes the requirement's page under its current key. */
export const permalinkRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/r/$requirementId",
  component: function PermalinkPage() {
    const { requirementId } = permalinkRoute.useParams();
    const { data: requirement, error, refetch } = useRequirementById(requirementId);
    const navigate = useNavigate();
    useEffect(() => {
      if (requirement) {
        void navigate({
          to: "/projects/$projectKey/r/$reqKey",
          params: { projectKey: requirement.projectKey, reqKey: requirement.key },
          hash: true,
          replace: true,
        });
      }
    }, [requirement, navigate]);
    if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
    return <Skeleton />;
  },
});

/** A count from the address, or nothing when it is no whole number of at least min. */
function count(value: unknown, min: number): number | undefined {
  const n = Number(value);
  return Number.isInteger(n) && n >= min ? n : undefined;
}

/** A requirement's history, where the search says which revision, comparison or window is open. */
export const historyRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/r/$reqKey/history",
  validateSearch: (search: Record<string, unknown>): HistorySearch => ({
    revision: count(search.revision, 1),
    from: count(search.from, 0),
    to: count(search.to, 0),
    offset: count(search.offset, 0),
    tab: search.tab === "activity" ? "activity" : undefined,
  }),
  component: function HistoryPage() {
    const { reqKey } = historyRoute.useParams();
    const search = historyRoute.useSearch();
    const navigate = useNavigate();
    const { data: requirement, error, refetch } = useRequirement(reqKey);
    const { data: project, error: projectError } = useProject(requirement?.projectKey ?? "");
    useEffect(() => {
      if (requirement && requirement.key !== reqKey) {
        void navigate({
          to: "/projects/$projectKey/r/$reqKey/history",
          params: { projectKey: requirement.projectKey, reqKey: requirement.key },
          search,
          replace: true,
        });
      }
    }, [requirement, reqKey, navigate, search]);
    if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
    if (projectError) return <ErrorBanner>{projectError.message}</ErrorBanner>;
    if (!requirement || !project) return <Skeleton />;
    return (
      <RequirementHistory
        project={project}
        requirement={requirement}
        search={search}
        onSearch={(next) =>
          void navigate({ to: "/projects/$projectKey/r/$reqKey/history", params: { projectKey: project.key, reqKey: requirement.key }, search: next })
        }
      />
    );
  },
});

/** A requirement's trace graph (#211), where the search says which way, how deep and through what it looks. */
export const traceGraphRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/r/$reqKey/trace",
  validateSearch: (search: Record<string, unknown>): GraphAddress => readGraphAddress(search),
  component: function TraceGraphRoutePage() {
    const { reqKey } = traceGraphRoute.useParams();
    const search = traceGraphRoute.useSearch();
    const navigate = useNavigate();
    const { data: requirement, error, refetch } = useRequirement(reqKey);
    const { data: project, error: projectError } = useProject(requirement?.projectKey ?? "");
    useEffect(() => {
      if (requirement && requirement.key !== reqKey) {
        void navigate({
          to: "/projects/$projectKey/r/$reqKey/trace",
          params: { projectKey: requirement.projectKey, reqKey: requirement.key },
          search,
          replace: true,
        });
      }
    }, [requirement, reqKey, navigate, search]);
    if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
    if (projectError) return <ErrorBanner>{projectError.message}</ErrorBanner>;
    if (!requirement || !project) return <Skeleton />;
    return (
      <TraceGraphPage
        key={requirement.id}
        project={project}
        requirement={requirement}
        address={search}
        onAddress={(next) =>
          void navigate({
            to: "/projects/$projectKey/r/$reqKey/trace",
            params: { projectKey: project.key, reqKey: requirement.key },
            search: next,
            replace: true,
          })
        }
      />
    );
  },
});
