import { Link } from "@tanstack/react-router";
import { useAddLabel, useRemoveLabel } from "@/api/labels";
import type { Requirement } from "@/api/requirements";
import { Card, ErrorBanner, IconButton, SectionTitle, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { LabelCombobox } from "./LabelCombobox";

/** One label as a way to the requirements of a project that carry it, or to those of every project. */
export function LabelLink({ name, projectKey, onRemove }: { name: string; projectKey?: string; onRemove?: () => void }) {
  const look = "inline-flex items-center gap-1 hover:text-ink hover:underline";
  return (
    <span
      className="inline-flex items-center gap-1 rounded border border-border bg-surface-raised px-1.5 py-px text-xs font-medium text-ink-muted"
      data-label={name}
    >
      {projectKey ? (
        <Link to="/p/$projectKey" params={{ projectKey }} search={{ label: name }} className={look}>
          <Icon.Label className="size-3" />
          {name}
        </Link>
      ) : (
        <Link to="/labels/$name" params={{ name }} search={{}} className={look}>
          <Icon.Label className="size-3" />
          {name}
        </Link>
      )}
      {onRemove && (
        <IconButton
          icon={<Icon.X className="size-3" />}
          label={t.labels.remove(name)}
          size="xs"
          onClick={onRemove}
          className="-my-0.5 -mr-1 text-current hover:bg-transparent hover:text-ink"
          data-action="remove-label"
        />
      )}
    </span>
  );
}

/** The labels on a requirement, each a way to the others that carry it, and for its editors a box to add more
 * and a way to take one off. */
export function RequirementLabels({ requirement }: { requirement: Requirement }) {
  const add = useAddLabel(requirement.key);
  const remove = useRemoveLabel(requirement.key);
  const toast = useToast();
  const editable = requirement.can.edit;
  const error = add.error ?? remove.error;

  return (
    <Card className="space-y-3 p-4" data-requirement-labels>
      <section aria-labelledby="labels-title" className="space-y-2">
        <SectionTitle id="labels-title">{t.labels.title}</SectionTitle>
        {requirement.labels.length > 0 ? (
          <ul className="flex flex-wrap gap-1.5" aria-labelledby="labels-title" data-label-list>
            {requirement.labels.map((name) => (
              <li key={name}>
                <LabelLink
                  name={name}
                  projectKey={requirement.projectKey}
                  onRemove={
                    editable
                      ? () => {
                          add.reset();
                          remove.mutate(name, { onSuccess: () => toast.success(t.labels.removed(name)) });
                        }
                      : undefined
                  }
                />
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-ink-subtle">{t.labels.none}</p>
        )}
        {editable && (
          <div className="max-w-xs">
            <LabelCombobox
              label={t.labels.add}
              exclude={requirement.labels}
              onPick={(name) => {
                remove.reset();
                add.mutate(name, { onSuccess: () => toast.success(t.labels.added(name)) });
              }}
            />
          </div>
        )}
        {error && <ErrorBanner>{error.message}</ErrorBanner>}
      </section>
    </Card>
  );
}
