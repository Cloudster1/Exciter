import { useState } from "react";
import type { Requirement } from "@/api/requirements";
import { useWatchProject, useWatchRequirement } from "@/api/watching";
import { Button, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { WatchersDialog } from "./WatchersDialog";

// Watching a requirement (#34), Stator's WatchMenu over a requirement and its project.

/** The requirement header's watch button: this requirement, or its whole project, and who else watches. */
export function WatchMenu({ requirement, onFailure }: { requirement: Requirement; onFailure: (failed: boolean) => void }) {
  const watchRequirement = useWatchRequirement(requirement.key);
  const watchProject = useWatchProject(requirement.projectKey);
  const [showWatchers, setShowWatchers] = useState(false);
  const { watching } = requirement;
  const told = { onError: () => onFailure(true), onSuccess: () => onFailure(false) };

  const items: MenuItem[] = [
    watching.requirement
      ? { label: t.watch.stopRequirement, onSelect: () => watchRequirement.mutate(false, told), attrs: { "data-action": "unwatch-requirement" } }
      : { label: t.watch.requirement, onSelect: () => watchRequirement.mutate(true, told), attrs: { "data-action": "watch-requirement" } },
    watching.project
      ? { label: t.watch.stopProject, onSelect: () => watchProject.mutate(false, told), attrs: { "data-action": "unwatch-project" } }
      : { label: t.watch.project, onSelect: () => watchProject.mutate(true, told), attrs: { "data-action": "watch-project" } },
  ];
  // A disabled item stays in the arrow order, so the reader hears what covers the requirement.
  if (watching.project && !watching.requirement) {
    items.push({ label: t.watch.coveredByProject, onSelect: () => {}, disabled: true, attrs: { "data-watch-covered": "" } });
  }
  items.push({ label: t.watch.watchers, icon: <Icon.Users />, onSelect: () => setShowWatchers(true), attrs: { "data-action": "show-watchers" } });
  const on = watching.requirement || watching.project;

  return (
    <>
      <Menu
        label={t.watch.menu(requirement.key)}
        align="end"
        items={items}
        trigger={(props) => (
          <Button
            variant="secondary"
            icon={<Icon.Eye />}
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            loading={watchRequirement.isPending || watchProject.isPending}
            data-action="watch-menu"
            data-watching={on || undefined}
          >
            {on ? t.watch.watching : t.watch.button}
          </Button>
        )}
      />
      {showWatchers && <WatchersDialog requirement={requirement} onClose={() => setShowWatchers(false)} />}
    </>
  );
}
