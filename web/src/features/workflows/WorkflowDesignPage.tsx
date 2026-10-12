import { Link, useNavigate } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { WorkflowDesigner } from "./designer/WorkflowDesigner";

/** The designer as a page of its own, wide enough for the canvas and the inspector side by side. */
export function WorkflowDesignPage({ project, workflowId }: { project: Project; workflowId?: string }) {
  const navigate = useNavigate();
  const back = () => navigate({ to: "/p/$projectKey/settings", params: { projectKey: project.key }, search: { tab: "workflow" } });
  const mayChange = project.can.administer && !project.archivedAt;
  return (
    <div className="space-y-5" data-workflow-design-page>
      <PageHeader
        crumb={
          <Link to="/p/$projectKey/settings" params={{ projectKey: project.key }} search={{ tab: "workflow" }} className="hover:text-accent hover:underline">
            {t.workflows.backToSettings}
          </Link>
        }
        title={workflowId ? t.workflows.designTitle : t.workflows.newTitle}
        meta={t.projects.projectAndKey(project.name, project.key)}
      />
      {mayChange ? (
        <WorkflowDesigner key={workflowId ?? "new"} projectKey={project.key} workflowId={workflowId} onDone={() => void back()} onCancel={() => void back()} />
      ) : (
        <p className="text-sm text-ink-muted">{t.projects.notProjectAdmin}</p>
      )}
    </div>
  );
}
