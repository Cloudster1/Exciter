import { useState } from "react";
import { useLinkTypeActions, useOrgLinkTypes, useProjectLinkTypes, type LinkType } from "@/api/links";
import type { Project } from "@/api/projects";
import { Button, Card, Checkbox, ErrorBanner, PageHeader, Skeleton, Table, Tag, Td, Th, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { LinkTypeForm, PairsDialog } from "./LinkTypeForm";

/** The table's columns, which a type opened for editing spans: a project's list adds whether it uses each. */
const COLUMNS = 4;
const PROJECT_COLUMNS = 5;

/** The installation's link types, which its administrators make, change, retire and delete. */
export function OrgLinkTypes() {
  const { data: types, error, refetch } = useOrgLinkTypes();
  return (
    <Page width="narrow" className="space-y-6" data-org-link-types>
      <PageHeader title={t.linkTypes.title} />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!types && !error && <Skeleton />}
      {types && <LinkTypeTable types={types} intro={t.linkTypes.orgIntro} mayCreate mayShape={() => true} />}
    </Page>
  );
}

/** The link types a project uses: its administrators turn each on or off, limit it to pairs of requirement
 * types, and shape the project's own. */
export function ProjectLinkTypes({ project }: { project: Project }) {
  const { data: types, error, refetch } = useProjectLinkTypes(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!types) return <Skeleton />;
  const mayChange = project.can.administer && !project.archivedAt;
  return (
    <LinkTypeTable
      types={types}
      intro={t.linkTypes.projectIntro}
      project={mayChange ? project : undefined}
      shown={project}
      mayCreate={mayChange}
      mayShape={(type) => mayChange && type.scope === "project"}
    />
  );
}

function LinkTypeTable({
  types,
  intro,
  project,
  shown,
  mayCreate,
  mayShape,
}: {
  types: LinkType[];
  intro: string;
  /** The project whose use of each type its administrator changes here. */
  project?: Project;
  /** The project the list is of, which shows each type's use there. */
  shown?: Project;
  mayCreate: boolean;
  mayShape: (type: LinkType) => boolean;
}) {
  const actions = useLinkTypeActions(project?.key);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [pairs, setPairs] = useState<LinkType | null>(null);
  const failed = actions.update.error ?? actions.remove.error ?? actions.use.error;
  const columns = shown ? PROJECT_COLUMNS : COLUMNS;
  return (
    <div className="space-y-4" data-link-type-list>
      <p className="text-sm text-ink-muted">{intro}</p>
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      <Table aria-label={t.linkTypes.tableLabel}>
        <thead>
          <tr>
            <Th>{t.linkTypes.columnType}</Th>
            <Th>{t.linkTypes.columnReads}</Th>
            <Th className="w-24">{t.linkTypes.columnLinks}</Th>
            {shown && <Th>{t.linkTypes.columnUse}</Th>}
            <Th>
              <span className="sr-only">{t.linkTypes.columnActions}</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {types.map((type) =>
            editing === type.id ? (
              <tr key={type.id} data-link-type={type.key}>
                <Td colSpan={columns}>
                  <LinkTypeForm projectKey={project?.key} type={type} onDone={() => setEditing(null)} />
                </Td>
              </tr>
            ) : (
              <tr key={type.id} data-link-type={type.key} data-retired={type.retired ? "" : undefined}>
                <Td>
                  <span className="flex flex-wrap items-center gap-2 font-medium text-ink">
                    {type.name}
                    <span className="font-mono text-xs text-ink-subtle">{type.key}</span>
                    {shown && <Tag>{type.scope === "project" ? t.linkTypes.ofProject : t.linkTypes.ofOrg}</Tag>}
                    {type.acyclic && <Tag>{t.linkTypes.acyclicTag}</Tag>}
                    {type.retired && <Tag>{t.linkTypes.retired}</Tag>}
                  </span>
                </Td>
                <Td>
                  <span className="text-ink-muted">{t.linkTypes.reads(type.forward, type.backward)}</span>
                </Td>
                <Td>
                  <span className="text-ink-muted tabular-nums">{type.links}</span>
                </Td>
                {shown && (
                  <Td>
                    <div className="space-y-1">
                      <Checkbox
                        label={<span className="sr-only">{t.linkTypes.use(type.name)}</span>}
                        checked={type.enabled}
                        disabled={!project || actions.use.isPending}
                        onChange={(event) => actions.use.mutate({ id: type.id, enabled: event.target.checked, rules: type.rules })}
                        data-link-type-enabled={type.key}
                      />
                      <span className="block text-xs text-ink-subtle" data-link-type-pairs={type.rules.length}>
                        {type.rules.length === 0 ? t.linkTypes.anyPair : t.linkTypes.pairs(type.rules.length)}
                      </span>
                    </div>
                  </Td>
                )}
                <Td className="text-right whitespace-nowrap">
                  {project && (
                    <Button variant="ghost" size="sm" onClick={() => setPairs(type)} data-action="edit-pairs">
                      {t.linkTypes.editPairs}
                    </Button>
                  )}
                  {mayShape(type) && (
                    <>
                      <Button variant="ghost" size="sm" onClick={() => setEditing(type.id)} data-action="edit-link-type">
                        {t.linkTypes.edit}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => actions.update.mutate({ id: type.id, retired: !type.retired })}
                        data-action={type.retired ? "restore-link-type" : "retire-link-type"}
                      >
                        {type.retired ? t.linkTypes.unretire : t.linkTypes.retire}
                      </Button>
                      {type.links === 0 && (
                        <Button variant="ghost" size="sm" onClick={() => actions.remove.mutate(type.id)} data-action="delete-link-type">
                          {t.linkTypes.delete}
                        </Button>
                      )}
                    </>
                  )}
                </Td>
              </tr>
            ),
          )}
        </tbody>
      </Table>
      {pairs && project && <PairsDialog projectKey={project.key} type={pairs} onClose={() => setPairs(null)} />}
      {mayCreate &&
        (adding ? (
          <Card className="p-4">
            <LinkTypeForm projectKey={project?.key} onDone={() => setAdding(false)} />
          </Card>
        ) : (
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-link-type">
            {t.linkTypes.create}
          </Button>
        ))}
    </div>
  );
}
