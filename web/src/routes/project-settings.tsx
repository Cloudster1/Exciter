import type { ComponentType } from "react";
import { createRoute, redirect, useParams } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { NEW_WORKFLOW } from "@/config";
import { ImportPanel } from "@/features/import/ImportPanel";
import { ProjectLinkTypes } from "@/features/links/LinkTypeList";
import { ProjectPermissions } from "@/features/permissions/ProjectPermissions";
import { ApprovalSection, DetailsSection, ProjectSettingsLayout, QualitySection, TraceSection } from "@/features/projects/ProjectSettings";
import { movedProjectAddress, settingsAddress, type ProjectSettingsSection } from "@/features/projects/settingsSections";
import { ReqIFPanel } from "@/features/reqif/ReqIFPanel";
import { TrashSection } from "@/features/requirements/TrashSection";
import { RiskSchemeCard } from "@/features/risks/RiskSchemeCard";
import { TemplateSettings } from "@/features/templates/TemplateSettings";
import { AttributeList } from "@/features/types/AttributeList";
import { FixedFieldRules } from "@/features/types/FixedFieldRules";
import { TypeList } from "@/features/types/TypeList";
import { WordImportPanel } from "@/features/word/WordImportPanel";
import { WorkflowDesignPage } from "@/features/workflows/WorkflowDesignPage";
import { WorkflowSettings } from "@/features/workflows/WorkflowSettings";
import { appRoute } from "./app";
import { ProjectPage } from "./projects";
import { rootRoute } from "./root";

/**
 * A project's address from before projects moved to /projects/, which mails,
 * Stator's and Armature's links and bookmarks still hold: sent on with its
 * search and hash, before sign-in so the way back after it is the new one.
 */
export const movedProjectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/p/$",
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: movedProjectAddress(params._splat ?? "", location.searchStr, location.hash), replace: true });
  },
});

/** The settings themselves open on Details, or on the section an old ?tab= named. */
export const projectSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings",
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: settingsAddress(params.projectKey, (location.search as Record<string, unknown>).tab, location.hash), replace: true });
  },
});

// Each section is a route of its own, so it can be linked, survives a reload and steps back with Back.
function show(name: ProjectSettingsSection, Body: ComponentType<{ project: Project }>) {
  return function ProjectSettingsSectionPage() {
    const { projectKey = "" } = useParams({ strict: false });
    return (
      <ProjectPage
        projectKey={projectKey}
        to={`/projects/$projectKey/settings/${name}`}
        render={(project) => (
          <ProjectSettingsLayout project={project} section={name}>
            <Body key={project.id} project={project} />
          </ProjectSettingsLayout>
        )}
      />
    );
  };
}

export const projectSettingsDetailsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/details",
  component: show("details", DetailsSection),
});

export const projectSettingsTypesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/types",
  component: show("types", TypeList),
});

export const projectSettingsAttributesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/attributes",
  component: show("attributes", AttributeList),
});

export const projectSettingsFieldsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/fields",
  component: show("fields", FixedFieldRules),
});

export const projectSettingsWorkflowRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/workflow",
  component: show("workflow", WorkflowSettings),
});

export const projectSettingsApprovalRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/approval",
  component: show("approval", ApprovalSection),
});

export const projectSettingsLinksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/links",
  component: show("links", ProjectLinkTypes),
});

export const projectSettingsTraceRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/trace",
  component: show("trace", TraceSection),
});

export const projectSettingsQualityRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/quality",
  component: show("quality", QualitySection),
});

export const projectSettingsRisksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/risks",
  component: show("risks", RiskSchemeCard),
});

export const projectSettingsTemplatesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/templates",
  component: show("templates", TemplateSettings),
});

export const projectSettingsPermissionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/permissions",
  component: show("permissions", ProjectPermissions),
});

export const projectSettingsImportRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/import",
  component: show("import", ImportPanel),
});

export const projectSettingsReqifRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/reqif",
  component: show("reqif", ReqIFPanel),
});

export const projectSettingsWordRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/word",
  component: show("word", WordImportPanel),
});

export const projectSettingsTrashRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/trash",
  component: show("trash", TrashSection),
});

export const projectSettingsSectionRoutes = [
  projectSettingsDetailsRoute,
  projectSettingsTypesRoute,
  projectSettingsAttributesRoute,
  projectSettingsFieldsRoute,
  projectSettingsWorkflowRoute,
  projectSettingsApprovalRoute,
  projectSettingsLinksRoute,
  projectSettingsTraceRoute,
  projectSettingsQualityRoute,
  projectSettingsRisksRoute,
  projectSettingsTemplatesRoute,
  projectSettingsPermissionsRoute,
  projectSettingsImportRoute,
  projectSettingsReqifRoute,
  projectSettingsWordRoute,
  projectSettingsTrashRoute,
];

/** The designer, wide, on one of a project's workflows or on a new one, below the Workflow section. */
export const workflowDesignRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey/settings/workflows/$workflowId",
  component: function WorkflowDesignRoute() {
    const { projectKey, workflowId } = workflowDesignRoute.useParams();
    return (
      <ProjectPage
        projectKey={projectKey}
        to="/projects/$projectKey/settings/workflow"
        render={(project) => <WorkflowDesignPage key={project.id} project={project} workflowId={workflowId === NEW_WORKFLOW ? undefined : workflowId} />}
      />
    );
  },
});
