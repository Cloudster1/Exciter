import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { Project } from "@/api/projects";
import { useRequirement } from "@/api/requirements";
import { useSpec, useTree, type SpecEntry, type TreeNode } from "@/api/tree";
import { reqifExportHref } from "@/api/reqif";
import { Button, ButtonLink, EmptyState, ErrorBanner, PageHeader, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PickBox, SelectionBar, useMayCreateIssues, useSelection, type Selection } from "@/features/armature/CreateIssues";
import { COMMENTS_ID } from "@/features/comments/CommentsSection";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { RequirementEdit } from "@/features/requirements/RequirementPage";
import { t } from "@/i18n";
import { PlaceDialog, type Placed } from "./PlaceDialog";
import { RequirementTree } from "./RequirementTree";
import { SuspectBadge } from "@/features/links/Suspect";

/** A project read as one numbered document beside its tree; editors change an entry in place and move by dragging. */
export function SpecPage({ project }: { project: Project }) {
  const navigate = useNavigate();
  const tree = useTree(project.key);
  const spec = useSpec(project.key);
  const [moving, setMoving] = useState<Placed | null>(null);
  const selection = useSelection();
  const mayCreate = useMayCreateIssues(project);
  const entries = spec.data?.pages.flatMap((page) => page.entries) ?? [];
  const mayWrite = project.can.edit && !project.archivedAt;
  const create = () => navigate({ to: "/p/$projectKey/new", params: { projectKey: project.key } });
  const placed = (node: TreeNode): Placed => ({ id: node.id, key: node.key, title: node.title, projectKey: project.key });

  return (
    <div className="mx-auto max-w-6xl" data-spec={project.key}>
      <PageHeader
        title={t.tree.specTitle}
        meta={t.projects.projectAndKey(project.name, project.key)}
        actions={
          mayWrite && (
            <Button icon={<Icon.Plus />} onClick={create} data-action="new-requirement">
              {t.requirements.create}
            </Button>
          )
        }
      />
      {(tree.error ?? spec.error) && <ErrorBanner>{(tree.error ?? spec.error)!.message}</ErrorBanner>}
      {tree.data && tree.data.length === 0 ? (
        <EmptyState
          icon={<Icon.Folder />}
          title={t.requirements.emptyTitle}
          description={mayWrite ? t.requirements.emptyBody : t.requirements.emptyBodyReader}
        />
      ) : (
        <div className="grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)]">
          <nav aria-label={t.tree.navLabel} className="lg:sticky lg:top-4 lg:max-h-[80vh] lg:self-start lg:overflow-y-auto">
            {tree.data ? <RequirementTree project={project} nodes={tree.data} onMove={(node) => setMoving(placed(node))} /> : <Skeleton />}
          </nav>
          <article className="min-w-0 space-y-6" aria-label={t.tree.specTitle} data-spec-document>
            {entries.map((entry) => (
              <Entry key={entry.node.id} project={project} entry={entry} selection={mayCreate ? selection : undefined} />
            ))}
            {spec.isLoading && <Skeleton />}
            {spec.hasNextPage && (
              <Button variant="secondary" loading={spec.isFetchingNextPage} onClick={() => void spec.fetchNextPage()} data-action="spec-more">
                {t.tree.more}
              </Button>
            )}
          </article>
        </div>
      )}
      {mayCreate && <SelectionBar projectKey={project.key} selection={selection} />}
      {moving && <PlaceDialog placed={moving} mode="move" onClose={() => setMoving(null)} onDone={() => setMoving(null)} />}
    </div>
  );
}

function Entry({ project, entry, selection }: { project: Project; entry: SpecEntry; selection?: Selection }) {
  const [editing, setEditing] = useState(false);
  const queryClient = useQueryClient();
  const { node } = entry;
  const section = node.kind === "section";
  const Heading = node.depth === 0 ? "h2" : "h3";
  return (
    <section aria-labelledby={`spec-${node.id}`} className={cx("space-y-2", node.depth > 0 && "border-l border-border pl-4")} data-spec-entry={node.key}>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        {selection && !section && entry.can.link && <PickBox selection={selection} item={{ key: node.key, title: node.title, outline: node.outline }} />}
        <Heading id={`spec-${node.id}`} className={cx("font-semibold text-ink", section ? "text-lg" : "text-base")}>
          <span className="font-mono text-ink-muted" data-spec-outline>
            {node.outline}
          </span>{" "}
          {node.title}
        </Heading>
        <Link
          to="/p/$projectKey/r/$reqKey"
          params={{ projectKey: project.key, reqKey: node.key }}
          className="font-mono text-xs text-ink-subtle hover:text-accent"
        >
          {node.key}
        </Link>
        {entry.comments.open > 0 && (
          <Link
            to="/p/$projectKey/r/$reqKey"
            params={{ projectKey: project.key, reqKey: node.key }}
            hash={COMMENTS_ID}
            className="inline-flex items-center gap-1 text-xs text-ink-muted hover:text-accent"
            data-spec-comments={entry.comments.open}
          >
            <Icon.Comment className="size-3" aria-hidden="true" />
            {t.comments.open(entry.comments.open)}
          </Link>
        )}
        {entry.suspectLinks > 0 && <SuspectBadge count={entry.suspectLinks} />}
        {section && (
          <ButtonLink
            href={reqifExportHref(project.key, true, node.key)}
            download
            variant="ghost"
            size="sm"
            icon={<Icon.Download />}
            title={t.reqif.exportSectionHint(node.title)}
            aria-label={t.reqif.exportSectionHint(node.title)}
            data-action="export-section-reqif"
          >
            {t.reqif.exportSection}
          </ButtonLink>
        )}
        {entry.can.edit && !editing && (
          <Button variant="ghost" size="sm" onClick={() => setEditing(true)} data-action="edit-entry">
            {t.requirements.edit}
          </Button>
        )}
      </div>
      {editing ? (
        <InlineEdit
          project={project}
          reqKey={node.key}
          onDone={() => {
            setEditing(false);
            void Promise.all([queryClient.invalidateQueries({ queryKey: ["spec"] }), queryClient.invalidateQueries({ queryKey: ["tree"] })]);
          }}
        />
      ) : (
        <>
          <DocView doc={entry.statement as Doc} />
          {Boolean(entry.rationale) && (
            <div className="text-sm text-ink-muted">
              <span className="font-medium">{t.requirements.rationale}: </span>
              <DocView doc={entry.rationale as Doc} className="inline" />
            </div>
          )}
        </>
      )}
    </section>
  );
}

/** The requirement's own form, read fresh so the save is made over its latest revision. */
function InlineEdit({ project, reqKey, onDone }: { project: Project; reqKey: string; onDone: () => void }) {
  const { data: requirement } = useRequirement(reqKey);
  if (!requirement) return <Skeleton />;
  return <RequirementEdit project={project} requirement={requirement} onDone={onDone} onCancel={onDone} />;
}
