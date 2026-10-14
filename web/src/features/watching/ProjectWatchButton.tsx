import type { Project } from "@/api/projects";
import { useWatchProject } from "@/api/watching";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Watches every requirement of a project, now and later, or stops; pressed while the reader watches it. */
export function ProjectWatchButton({ project }: { project: Project }) {
  const watch = useWatchProject(project.key);
  return (
    <Button
      variant="secondary"
      icon={<Icon.Eye />}
      aria-pressed={project.watching}
      title={t.watch.projectHint(project.name)}
      loading={watch.isPending}
      onClick={() => watch.mutate(!project.watching)}
      data-action="watch-project"
      data-watching={project.watching || undefined}
    >
      {project.watching ? t.watch.projectWatching : t.watch.projectButton}
    </Button>
  );
}
