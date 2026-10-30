import { useState } from "react";
import { permissionWords, useRoles } from "@/api/access";
import type { Project } from "@/api/projects";
import { Button, Card, PageHeader, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { RoleTable } from "@/features/access/RoleTable";
import { t } from "@/i18n";
import { AccessDialog } from "./AccessDialog";

/**
 * Who holds which role in a project: a role is granted to a person or a group,
 * Everyone among them, and taken away, one grant at a time. What a role holds
 * is the installation's matrix; here it is said in words.
 */
export function ProjectPermissions({ project }: { project: Project }) {
  const [inspecting, setInspecting] = useState(false);
  const archived = Boolean(project.archivedAt);
  return (
    <Page width="narrow" className="space-y-6" data-project-permissions={project.key}>
      <PageHeader title={t.permissions.title} meta={t.projects.projectAndKey(project.name, project.key)} />
      {!project.can.administer ? (
        <p className="text-sm text-ink-muted">{t.permissions.notProjectAdmin}</p>
      ) : (
        <>
          <p className="text-sm text-ink-muted">{t.permissions.intro}</p>
          {archived && <p className="text-sm text-ink-muted">{t.permissions.archived}</p>}
          <RoleTable projectKey={project.key} readOnly={archived} />
          <Card className="space-y-3 p-4">
            <RolesInWords />
            <p className="text-sm text-ink-muted">{t.permissions.orgAdminsNote}</p>
            <Button variant="secondary" icon={<Icon.Shield />} onClick={() => setInspecting(true)} data-action="check-access">
              {t.permissions.checkAccess}
            </Button>
          </Card>
          {inspecting && <AccessDialog project={project} onClose={() => setInspecting(false)} />}
        </>
      )}
    </Page>
  );
}

/** What each role a project grants holds, so the person granting knows what they give. */
function RolesInWords() {
  const { data: roles } = useRoles();
  const projectRoles = (roles ?? []).filter((role) => !role.orgWideOnly);
  if (projectRoles.length === 0) return null;
  return (
    <section className="space-y-2" data-project-roles>
      <h2 className="text-sm font-semibold text-ink">{t.permissions.rolesTitle}</h2>
      <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]" data-role-legend>
        {projectRoles.map((role) => (
          <div key={role.role} className="contents" data-project-role={role.role}>
            <dt className="font-medium text-ink">{role.name}</dt>
            <dd className="text-ink-muted">
              {role.permissions
                .filter((permission) => permission !== "createProjects" && permission !== "administerInstallation")
                .map((permission) => permissionWords(permission))
                .join(", ")}
            </dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-ink-subtle">{t.permissions.rolesHint}</p>
    </section>
  );
}
