import { useState } from "react";
import type { Project } from "@/api/projects";
import { useTrash, useTrashActions, type TrashItem } from "@/api/requirements";
import { Button, Dialog, ErrorBanner, PageHeader, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const trashedAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** A project's trash: its editors restore, its administrators delete for good, one or all. */
export function TrashPage({ project }: { project: Project }) {
  const mayOpen = project.can.edit;
  const mayPurge = project.can.purge;
  const { data, error, refetch } = useTrash(project.key, mayOpen);
  const actions = useTrashActions(project.key);
  const [notice, setNotice] = useState("");
  const [purging, setPurging] = useState<TrashItem | "all" | null>(null);
  const failed = actions.restore.error ?? actions.purge.error ?? actions.empty.error;

  return (
    <div className="mx-auto max-w-4xl space-y-4" data-trash={project.key}>
      <PageHeader
        title={t.requirements.trashTitle}
        meta={t.projects.projectAndKey(project.name, project.key)}
        actions={
          mayPurge &&
          data &&
          data.length > 0 && (
            <Button variant="danger" icon={<Icon.Trash />} onClick={() => setPurging("all")} data-action="empty-trash">
              {t.requirements.emptyTrash}
            </Button>
          )
        }
      />
      {!mayOpen ? (
        <p className="text-sm text-ink-muted">{t.requirements.readOnly}</p>
      ) : (
        <>
          <p className="text-sm text-ink-muted">{t.requirements.trashIntro}</p>
          {!mayPurge && <p className="text-sm text-ink-muted">{t.requirements.purgeOnlyAdmins}</p>}
          <p role="status" className="text-sm text-ink-muted empty:hidden">
            {notice}
          </p>
          {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
          {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
          {!data && !error && <Skeleton />}
          {data && data.length === 0 && <p className="text-sm text-ink-subtle">{t.requirements.trashEmpty}</p>}
          {data && data.length > 0 && (
            <Table aria-label={t.requirements.trashLabel}>
              <thead>
                <tr>
                  <Th className="w-28">{t.requirements.columnKey}</Th>
                  <Th>{t.requirements.columnTitle}</Th>
                  <Th>
                    <span className="sr-only">{t.requirements.restore}</span>
                  </Th>
                </tr>
              </thead>
              <tbody>
                {data.map((item) => (
                  <tr key={item.id} data-trash-item={item.key}>
                    <Td>
                      <span className="font-mono text-xs text-ink-muted">{item.key}</span>
                    </Td>
                    <Td>
                      <span className="block font-medium text-ink">{item.title}</span>
                      <span className="text-xs text-ink-subtle">
                        {t.requirements.trashedBy(item.trashedByName, trashedAt.format(new Date(item.trashedAt)))}
                      </span>
                    </Td>
                    <Td className="text-right whitespace-nowrap">
                      <Button
                        variant="secondary"
                        size="sm"
                        loading={actions.restore.isPending && actions.restore.variables === item.id}
                        onClick={() => actions.restore.mutate(item.id, { onSuccess: () => setNotice(t.requirements.restored(item.key)) })}
                        data-action="restore-requirement"
                      >
                        {t.requirements.restore}
                      </Button>
                      {mayPurge && (
                        <Button variant="ghost" size="sm" className="ml-1" onClick={() => setPurging(item)} data-action="purge-requirement">
                          {t.requirements.purge}
                        </Button>
                      )}
                    </Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </>
      )}
      {purging && (
        <Dialog
          open
          title={purging === "all" ? t.requirements.emptyTrashTitle : t.requirements.purgeTitle}
          onClose={() => setPurging(null)}
          attrs={{ "data-confirm-purge": "" }}
        >
          <p className="text-sm text-ink">{purging === "all" ? t.requirements.emptyTrashBody : t.requirements.purgeBody(purging.key)}</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setPurging(null)}>
              {t.kit.cancel}
            </Button>
            <Button
              variant="danger"
              loading={actions.purge.isPending || actions.empty.isPending}
              onClick={() => {
                const done = { onSettled: () => setPurging(null) };
                if (purging === "all") actions.empty.mutate(undefined, done);
                else actions.purge.mutate(purging.id, done);
              }}
              data-action="confirm-purge"
            >
              {t.requirements.purge}
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  );
}
