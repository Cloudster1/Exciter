import { Link } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { Table, Tag, Td, Th } from "@/components/ui";
import { t } from "@/i18n";

/** Projects by name with their keys, each a link into the project. */
export function ProjectTable({ projects, label }: { projects: Project[]; label: string }) {
  return (
    <Table aria-label={label}>
      <thead>
        <tr>
          <Th>{t.projects.columnName}</Th>
          <Th className="w-28">{t.projects.columnKey}</Th>
          <Th>{t.projects.columnDescription}</Th>
        </tr>
      </thead>
      <tbody>
        {projects.map((project) => (
          <tr key={project.id} data-project-row={project.key} data-archived={project.archivedAt ? "" : undefined}>
            <Td>
              <Link to="/p/$projectKey" params={{ projectKey: project.key }} className="font-medium text-ink hover:text-accent hover:underline">
                {project.name}
              </Link>
              {project.archivedAt && <Tag className="ml-2 align-middle">{t.projects.archived}</Tag>}
            </Td>
            <Td>
              <span className="font-mono text-xs text-ink-muted">{project.key}</span>
            </Td>
            <Td>
              <span className="text-ink-muted">{project.description}</span>
            </Td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
