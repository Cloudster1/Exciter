import type { Project } from "@/api/projects";
import { useWatchProject } from "@/api/watching";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Watches every requirement of a project, now and later, or stops; pressed while the reader watches it. A failure is
 * the page's to say, as the requirement's watch menu leaves it to its page. */
export function ProjectWatchButton({ project, onFailure }: { project: Project; onFailure: (failed: boolean) => void }) {
  const watch = useWatchProject(project.key);
  return (
    <Button
      variant="secondary"
      icon={<Icon.Eye />}
      aria-pressed={project.watching}
      title={t.watch.projectHint(project.name)}
      loading={watch.isPending}
      onClick={() => watch.mutate(!project.watching, { onError: () => onFailure(true), onSuccess: () => onFailure(false) })}
      data-action="watch-project"
      data-watching={project.watching || undefined}
    >
      {project.watching ? t.watch.projectWatching : t.watch.projectButton}
    </Button>
  );
}
