import { useEffect } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { useProject } from "@/api/projects";
import { useRequirement, useRequirementById } from "@/api/requirements";
import { ErrorBanner, Skeleton } from "@/components/ui";
import { RequirementPage } from "@/features/requirements/RequirementPage";
import { appRoute } from "./app";

/** A requirement's page. A key it had, or its project had, still finds it, and the address follows to the current one. */
export const requirementRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/p/$projectKey/r/$reqKey",
  component: function RequirementRoutePage() {
    const { reqKey } = requirementRoute.useParams();
    const { data: requirement, error, refetch } = useRequirement(reqKey);
    const { data: project, error: projectError } = useProject(requirement?.projectKey ?? "");
    const navigate = useNavigate();
    useEffect(() => {
      if (requirement && requirement.key !== reqKey) {
        void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: requirement.projectKey, reqKey: requirement.key }, replace: true });
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
        void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: requirement.projectKey, reqKey: requirement.key }, replace: true });
      }
    }, [requirement, navigate]);
    if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
    return <Skeleton />;
  },
});
