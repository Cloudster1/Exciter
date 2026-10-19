import { useNavigate, useParams } from "@tanstack/react-router";
import { useProject, useProjects } from "@/api/projects";
import { Button, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PROJECTS_PATH } from "@/config";
import { NavItem } from "@/features/shell/nav";
import { SidebarGroup } from "@/features/shell/SidebarGroup";
import { t } from "@/i18n";

/** The project the reader is in, in the sidebar: a switcher to the others, and the project's pages. */
export function ProjectNav({ open, onToggle, onNavigate }: { open: boolean; onToggle: () => void; onNavigate?: () => void }) {
  const { projectKey } = useParams({ strict: false });
  const { data: project } = useProject(projectKey ?? "");
  const { data: projects } = useProjects(false, Boolean(project));
  const navigate = useNavigate();
  if (!projectKey || !project) return null;

  const go = (to: () => Promise<void>) => () => {
    onNavigate?.();
    void to();
  };
  const others = (projects ?? []).filter((each) => each.id !== project.id);
  const items: MenuItem[] = [
    ...others.map((each) => ({
      label: `${each.name} (${each.key})`,
      onSelect: go(() => navigate({ to: "/p/$projectKey", params: { projectKey: each.key } })),
      attrs: { "data-switch-to": each.key },
    })),
    ...(projects && others.length === 0 ? [{ label: t.projects.noOtherProjects, onSelect: () => {}, disabled: true }] : []),
    ...(projects ? [] : [{ label: t.projects.loadingProjects, onSelect: () => {}, disabled: true }]),
    { label: t.projects.allProjects, icon: <Icon.Folder />, onSelect: go(() => navigate({ to: PROJECTS_PATH })), attrs: { "data-switch-to": "all" } },
  ];

  return (
    <SidebarGroup id="project" title={project.name} open={open} onToggle={onToggle}>
      <div data-project-nav={project.key}>
        <Menu
          label={t.projects.switcher}
          items={items}
          className="mb-1 w-full"
          trigger={({ open: _open, toggle, ...menu }) => (
            <Button
              {...menu}
              variant="ghost"
              size="sm"
              onClick={toggle}
              className="w-full justify-between px-2!"
              iconRight={<Icon.ChevronDown className="shrink-0" />}
              data-project-switcher=""
            >
              <span className="min-w-0 truncate">
                <span className="font-mono text-xs text-ink-muted">{project.key}</span> {t.projects.switcher}
              </span>
            </Button>
          )}
        />
        <NavItem to={`/p/${project.key}`} icon="Folder" rail={false} onNavigate={onNavigate} exact>
          {t.requirements.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/spec`} icon="Table" rail={false} onNavigate={onNavigate}>
          {t.tree.specTitle}
        </NavItem>
        <NavItem to={`/p/${project.key}/grid`} icon="Table" rail={false} onNavigate={onNavigate}>
          {t.grid.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/baselines`} icon="Archive" rail={false} onNavigate={onNavigate}>
          {t.baseline.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/tests`} icon="Check" rail={false} onNavigate={onNavigate}>
          {t.tests.nav}
        </NavItem>
        <NavItem to={`/p/${project.key}/risks`} icon="Warning" rail={false} onNavigate={onNavigate}>
          {t.risks.nav}
        </NavItem>
        <NavItem to={`/p/${project.key}/sessions`} icon="Check" rail={false} onNavigate={onNavigate}>
          {t.sessions.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/coverage`} icon="Link" rail={false} onNavigate={onNavigate}>
          {t.coverage.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/dashboards`} icon="Monitor" rail={false} onNavigate={onNavigate}>
          {t.dashboard.nav}
        </NavItem>
        <NavItem to={`/p/${project.key}/quality`} icon="Check" rail={false} onNavigate={onNavigate}>
          {t.quality.pageTitle}
        </NavItem>
        <NavItem to={`/p/${project.key}/glossary`} icon="Book" rail={false} onNavigate={onNavigate}>
          {t.glossary.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/duplicates`} icon="Copy" rail={false} onNavigate={onNavigate}>
          {t.duplicates.page.title}
        </NavItem>
        <NavItem to={`/p/${project.key}/suspects`} icon="Warning" rail={false} onNavigate={onNavigate}>
          {t.suspect.listTitle}
        </NavItem>
        <NavItem to={`/p/${project.key}/code-tags`} icon="Warning" rail={false} onNavigate={onNavigate}>
          {t.evidence.codeTags.title}
        </NavItem>
        {project.can.edit && (
          <NavItem to={`/p/${project.key}/trash`} icon="Trash" rail={false} onNavigate={onNavigate}>
            {t.requirements.trashTitle}
          </NavItem>
        )}
        <NavItem to={`/p/${project.key}/settings`} icon="Settings" rail={false} onNavigate={onNavigate}>
          {t.projects.settings}
        </NavItem>
        {project.can.administer && (
          <NavItem to={`/p/${project.key}/permissions`} icon="Shield" rail={false} onNavigate={onNavigate}>
            {t.permissions.navLabel}
          </NavItem>
        )}
      </div>
    </SidebarGroup>
  );
}
