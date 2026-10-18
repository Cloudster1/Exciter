import { Fragment, useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Link, useBlocker, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { Project } from "@/api/projects";
import type { Decision } from "@/api/reviews";
import { useSpec, useTree, type Place, type SpecAddress, type SpecEntry, type TreeNode } from "@/api/tree";
import { reqifExportHref } from "@/api/reqif";
import { projectDocumentHref } from "@/api/documents";
import { Button, Card, Dialog, EmptyState, ErrorBanner, Menu, PageHeader, Skeleton, Tag, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SelectionBar, useMayCreateIssues, useSelection, type Picked, type Selection } from "@/features/armature/CreateIssues";
import { COMMENTS_ID } from "@/features/comments/CommentsSection";
import { DiffTitle, DocView, type DiffBlockView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { RequirementCreate } from "@/features/requirements/NewRequirementForm";
import { RequirementEdit, type Editable } from "@/features/requirements/RequirementEdit";
import { t } from "@/i18n";
import { OpenSessionButton, SessionBanner } from "@/features/sessions/SessionLaunch";
import { lockSentence } from "@/features/requirements/LockBanner";
import { narrowed } from "./address";
import { ChangesBar, ChangeTag, differs, EntryChanges, nextChange, RemovedSince } from "./DocumentChanges";
import { AskForReviews, EntryComments, EntryReview, progressOf, ReviewModeButton, withDecided } from "./DocumentReviews";
import { BulkTransitions, EntryPick, pickedOf, rangeOf, SectionPick, StatusFilter, StatusMenu } from "./DocumentStates";
import { PlaceDialog, type Placed } from "./PlaceDialog";
import { RequirementTree } from "./RequirementTree";
import { SuspectBadge } from "@/features/links/Suspect";
import { ExportMenu } from "@/features/print/ExportMenu";

/** What the document has open: one entry's editor, or a new requirement after an entry or first below it. */
export type Open = { kind: "edit"; key: string } | { kind: "add"; key: string; where: "after" | "below" };

/** Where a new requirement goes for an entry: right after it, or first below it. */
export function addedPlace(node: TreeNode, where: "after" | "below"): Place {
  return where === "after" ? { parentId: node.parentId, afterId: node.id } : { parentId: node.id, afterId: null };
}

/** What an entry's editor starts from, as the requirement's page would read it. */
export function editableOf(entry: SpecEntry): Editable {
  const { node } = entry;
  return {
    key: node.key,
    kind: node.kind,
    title: node.title,
    statement: entry.statement,
    rationale: entry.rationale,
    type: entry.type,
    attributes: entry.attributes,
    revision: node.revision,
    reuse: entry.reuse,
    status: node.status,
  };
}

/** The last entry of key's subtree among those loaded: a requirement added after key reads after it. */
export function lastBelow(entries: SpecEntry[], key: string): string {
  const at = entries.findIndex((entry) => entry.node.key === key);
  const depth = entries[at]?.node.depth;
  if (depth === undefined) return key;
  let last = key;
  for (const entry of entries.slice(at + 1)) {
    if (entry.node.depth <= depth) break;
    last = entry.node.key;
  }
  return last;
}

/** The entry the keyboard moves to from the one focused: the next or previous, or the first or last. */
export function stepTo(keys: string[], from: string, key: string): string | undefined {
  const at = keys.indexOf(from);
  if (at < 0) return undefined;
  switch (key) {
    case "ArrowDown":
      return keys[Math.min(at + 1, keys.length - 1)];
    case "ArrowUp":
      return keys[Math.max(at - 1, 0)];
    case "Home":
      return keys[0];
    case "End":
      return keys[keys.length - 1];
  }
  return undefined;
}

/** Entries below a section's, among those loaded, up to the next at its depth or above. */
export function entriesBelow(entries: SpecEntry[], key: string): SpecEntry[] {
  const at = entries.findIndex((entry) => entry.node.key === key);
  const depth = entries[at]?.node.depth;
  if (depth === undefined) return [];
  const out: SpecEntry[] = [];
  for (const entry of entries.slice(at + 1)) {
    if (entry.node.depth <= depth) break;
    out.push(entry);
  }
  return out;
}

/** A project read as one numbered document beside its tree; editors change and add entries in place and move by
 * dragging, and whoever may move requirements through their workflow does so one at a time or many at once. */
export function SpecPage({ project, address = {}, onAddress }: { project: Project; address?: SpecAddress; onAddress?: (next: SpecAddress) => void }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const tree = useTree(project.key);
  const spec = useSpec(project.key, address);
  const [moving, setMoving] = useState<Placed | null>(null);
  const [open, setOpen] = useState<Open | null>(null);
  const [dirty, setDirty] = useState(false);
  // An open asked for while the draft has changes, waiting for the reader to discard them.
  const [asked, setAsked] = useState<{ next: Open | null } | null>(null);
  const [focused, setFocused] = useState<string | null>(null);
  const [announced, setAnnounced] = useState("");
  const documentRef = useRef<HTMLElement>(null);
  const selection = useSelection();
  const mayCreate = useMayCreateIssues(project);
  // The entry last picked, from which a pick with Shift reaches.
  const lastPick = useRef<string | null>(null);
  const loaded = spec.data?.pages.flatMap((page) => page.entries) ?? [];
  // What the reader decided in review mode stays in view, marked, until they leave it.
  const [decided, setDecided] = useState<Map<string, SpecEntry>>(new Map());
  const reviewing = Boolean(address.waiting);
  const entries = reviewing ? withDecided(loaded, decided) : loaded;
  useEffect(() => {
    if (!reviewing && decided.size > 0) {
      setDecided(new Map());
      void queryClient.invalidateQueries({ queryKey: ["spec"] });
    }
  }, [reviewing, decided, queryClient]);
  const summary = spec.data?.pages[0]?.changes ?? null;
  const keys = entries.map((entry) => entry.node.key);
  const mayWrite = project.can.edit && !project.archivedAt;
  const create = () => navigate({ to: "/p/$projectKey/new", params: { projectKey: project.key } });
  const selectable = (entry: SpecEntry) => entry.node.kind !== "section" && (entry.can.transition || (mayCreate && entry.can.link));
  const pickable = entries.filter(selectable);
  const mayMove = pickable.some((entry) => entry.can.transition);
  const pick = (key: string, on: boolean, range: boolean) => {
    const keys =
      range && lastPick.current
        ? rangeOf(
            pickable.map((entry) => entry.node.key),
            lastPick.current,
            key,
          )
        : [key];
    selection.set(pickable.filter((entry) => keys.includes(entry.node.key)).map(pickedOf), on);
    lastPick.current = key;
  };
  const placed = (node: TreeNode): Placed => ({ id: node.id, key: node.key, title: node.title, projectKey: project.key });
  const anchor = open?.kind === "add" && open.where === "after" ? entries.find((entry) => entry.node.key === open.key) : undefined;
  const addAfter = anchor ? lastBelow(entries, anchor.node.key) : undefined;
  const added = (made: string) => {
    setOpen(null);
    setDirty(false);
    setAnnounced(t.tree.added(made));
    void refresh().then(() => focusEntry(made));
  };
  const leaving = useBlocker({ shouldBlockFn: () => dirty, enableBeforeUnload: () => dirty, withResolver: true });

  const focusEntry = useCallback((key: string) => {
    setFocused(key);
    // The entry renders before it takes focus, so the reader lands on it as it now reads.
    requestAnimationFrame(() => documentRef.current?.querySelector<HTMLElement>(`[data-spec-entry="${key}"]`)?.focus());
  }, []);

  const refresh = () => Promise.all([queryClient.invalidateQueries({ queryKey: ["spec"] }), queryClient.invalidateQueries({ queryKey: ["tree"] })]);

  /** Opens an editor or closes the one open; a closed one gives focus back to its entry, and each step is announced. */
  function begin(next: Open | null, notice?: string) {
    const left = open?.key;
    setOpen(next);
    setDirty(false);
    if (next?.kind === "edit") setAnnounced(t.tree.editing(next.key));
    else if (next?.kind === "add") setAnnounced(next.where === "after" ? t.tree.addingAfter(next.key) : t.tree.addingBelow(next.key));
    else if (left) {
      setAnnounced(notice ?? t.tree.left(left));
      focusEntry(left);
    }
  }

  /** Opens an editor, or closes the one open, once the reader has let go of unsaved changes. */
  function ask(next: Open | null) {
    if (dirty) setAsked({ next });
    else begin(next);
  }

  /** Goes to the next or previous changed entry from the one focused, or says there is none. */
  function step(direction: 1 | -1) {
    const to = nextChange(entries, focused, direction);
    if (to) focusEntry(to);
    else setAnnounced(t.tree.noMoreChanges);
  }

  function keyDown(event: KeyboardEvent<HTMLElement>) {
    const target = event.target as HTMLElement;
    const key = target.dataset.specEntry;
    // Only the entry itself steers: keys typed into its editor, links or buttons are theirs.
    if (!key || event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return;
    if (address.changes && (event.key === "n" || event.key === "p") && !event.shiftKey) {
      event.preventDefault();
      step(event.key === "n" ? 1 : -1);
      return;
    }
    const next = stepTo(keys, key, event.key);
    if (next) {
      event.preventDefault();
      focusEntry(next);
      return;
    }
    const entry = entries.find((each) => each.node.key === key);
    if (!entry) return;
    if ((event.key === "Enter" || event.key === "e") && entry.can.edit) {
      event.preventDefault();
      ask({ kind: "edit", key });
    } else if (event.key === "a" && mayWrite) {
      event.preventDefault();
      ask({ kind: "add", key, where: "after" });
    }
  }

  return (
    <div className="mx-auto max-w-6xl" data-spec={project.key}>
      <PageHeader
        title={t.tree.specTitle}
        meta={t.projects.projectAndKey(project.name, project.key)}
        actions={
          <div className="flex flex-wrap gap-2">
            <ExportMenu
              title={project.name}
              projectKey={project.key}
              href={(format) => projectDocumentHref(project.key, format)}
              reqifHref={reqifExportHref(project.key, true)}
            />
            {onAddress && <ReviewModeButton address={address} onChange={onAddress} />}
            {mayWrite && (
              <Button icon={<Icon.Plus />} onClick={create} data-action="new-requirement">
                {t.requirements.create}
              </Button>
            )}
          </div>
        }
      />
      {address.session && <SessionBanner project={project} sessionId={address.session} />}
      {(tree.error ?? spec.error) && <ErrorBanner>{(tree.error ?? spec.error)!.message}</ErrorBanner>}
      <p role="status" aria-live="polite" className="sr-only" data-spec-announced>
        {announced}
      </p>
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
          {/* biome-ignore lint/a11y/noStaticElementInteractions: the keys steer between the entries, each of which is focusable */}
          <article
            ref={documentRef}
            className="min-w-0 space-y-6"
            aria-label={t.tree.specTitle}
            aria-describedby="spec-keys"
            onKeyDown={keyDown}
            data-spec-document
          >
            <p id="spec-keys" className="text-xs text-ink-subtle">
              {mayWrite ? t.tree.specKeys : t.tree.specKeysReader}
            </p>
            {reviewing && (
              <p className="text-sm text-ink-muted" data-reviewing>
                {t.tree.reviewing}{" "}
                <span className="font-medium text-ink" data-review-progress>
                  {t.tree.progress(progressOf(entries).decided, progressOf(entries).total)}
                </span>
              </p>
            )}
            {reviewing && spec.data && entries.length === 0 && (
              <p className="text-sm text-ink-muted" data-nothing-waiting>
                {t.tree.nothingWaiting}
              </p>
            )}
            {onAddress && <StatusFilter projectKey={project.key} address={address} onChange={onAddress} />}
            {onAddress && <ChangesBar projectKey={project.key} address={address} summary={summary} onChange={onAddress} onStep={step} />}
            {address.changes === "baseline" && !address.baseline && (
              <p className="text-sm text-ink-muted" data-pick-baseline-first>
                {t.tree.pickBaselineFirst}
              </p>
            )}
            {spec.data && entries.length === 0 && narrowed(address) && (
              <p className="text-sm text-ink-muted" data-spec-empty-filter>
                {t.tree.emptyFiltered}
              </p>
            )}
            {entries.map((entry, index) => (
              <Fragment key={entry.node.id}>
                <Entry
                  project={project}
                  entry={entry}
                  open={open?.key === entry.node.key ? open : null}
                  mayWrite={mayWrite}
                  tabStop={focused ? focused === entry.node.key : index === 0}
                  selection={pickable.length > 0 ? selection : undefined}
                  selectable={selectable(entry)}
                  below={entry.node.kind === "section" ? entriesBelow(entries, entry.node.key).filter(selectable).map(pickedOf) : []}
                  onPick={pick}
                  collapsed={Boolean(address.onlyChanged) && entry.node.kind === "requirement" && !differs(entry)}
                  changedBelow={
                    mayWrite && address.changes && entry.node.kind === "section"
                      ? entriesBelow(entries, entry.node.key).filter((each) => each.node.kind === "requirement" && differs(each) && !each.review)
                      : []
                  }
                  onDecided={(decision) => {
                    if (entry.review) setDecided(new Map(decided).set(entry.node.key, { ...entry, review: { ...entry.review, decision } }));
                    setAnnounced(`${entry.node.key}: ${t.tree.youDecided[decision]}`);
                  }}
                  onMoved={(notice) => {
                    setAnnounced(notice);
                    void refresh();
                  }}
                  onAsk={ask}
                  onFocus={() => setFocused(entry.node.key)}
                  onDirty={setDirty}
                  onSaved={(notice) => {
                    begin(null, notice);
                    void refresh();
                  }}
                  onConflict={() => void refresh()}
                  onAdded={added}
                />
                {anchor && addAfter === entry.node.key && (
                  <AddedHere
                    project={project}
                    place={addedPlace(anchor.node, "after")}
                    where={t.tree.addingAfter(anchor.node.key)}
                    onDirty={setDirty}
                    onAsk={ask}
                    onAdded={added}
                  />
                )}
              </Fragment>
            ))}
            {summary && <RemovedSince removed={summary.removed} />}
            {spec.isLoading && <Skeleton />}
            {spec.hasNextPage && (
              <Button variant="secondary" loading={spec.isFetchingNextPage} onClick={() => void spec.fetchNextPage()} data-action="spec-more">
                {t.tree.more}
              </Button>
            )}
          </article>
        </div>
      )}
      {(mayCreate || mayMove) && (
        <SelectionBar projectKey={project.key} selection={selection} armature={mayCreate}>
          {mayMove && <BulkTransitions projectKey={project.key} selection={selection} onDone={setAnnounced} />}
          {mayWrite && (
            <AskForReviews
              projectKey={project.key}
              entries={entries.filter((entry) => selection.has(entry.node.key) && !entry.review)}
              label={t.tree.askReviews}
              onDone={setAnnounced}
            />
          )}
        </SelectionBar>
      )}
      {moving && <PlaceDialog placed={moving} mode="move" onClose={() => setMoving(null)} onDone={() => setMoving(null)} />}
      {(asked || leaving.status === "blocked") && (
        <DiscardDialog
          onKeep={() => {
            setAsked(null);
            leaving.reset?.();
          }}
          onDiscard={() => {
            const next = asked?.next ?? null;
            setAsked(null);
            if (leaving.status === "blocked") {
              setDirty(false);
              leaving.proceed();
              return;
            }
            begin(next);
          }}
        />
      )}
    </div>
  );
}

function DiscardDialog({ onKeep, onDiscard }: { onKeep: () => void; onDiscard: () => void }) {
  return (
    <Dialog title={t.tree.discardTitle} onClose={onKeep} data-discard-dialog="">
      <p className="mb-4 text-sm text-ink-muted">{t.tree.discardBody}</p>
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onKeep} data-action="keep-editing">
          {t.tree.keepEditing}
        </Button>
        <Button variant="danger" onClick={onDiscard} data-action="discard-changes">
          {t.tree.discard}
        </Button>
      </div>
    </Dialog>
  );
}

function Entry({
  project,
  entry,
  open,
  mayWrite,
  tabStop,
  selection,
  selectable,
  below,
  onPick,
  onMoved,
  collapsed,
  changedBelow,
  onDecided,
  onAsk,
  onFocus,
  onDirty,
  onSaved,
  onConflict,
  onAdded,
}: {
  project: Project;
  entry: SpecEntry;
  open: Open | null;
  mayWrite: boolean;
  tabStop: boolean;
  selection?: Selection;
  selectable: boolean;
  below: Picked[];
  onPick: (key: string, on: boolean, range: boolean) => void;
  onMoved: (notice: string) => void;
  /** Shows the heading alone, for an entry that did not change while only changed ones are read. */
  collapsed: boolean;
  /** A section's changed requirements without a review waiting, which it asks a review of at once. */
  changedBelow: SpecEntry[];
  onDecided: (decision: Decision) => void;
  onAsk: (next: Open | null) => void;
  onFocus: () => void;
  onDirty: (dirty: boolean) => void;
  onSaved: (notice: string) => void;
  onConflict: () => void;
  onAdded: (key: string) => void;
}) {
  const { node } = entry;
  const section = node.kind === "section";
  const editing = open?.kind === "edit";
  const addingBelow = open?.kind === "add" && open.where === "below";
  const title = entry.change?.comparison?.title as DiffBlockView[] | undefined;
  const titleDiff = title?.some((block) => block.change !== "equal") ? title : undefined;
  const Heading = node.depth === 0 ? "h2" : "h3";
  return (
    <section
      aria-labelledby={`spec-${node.id}`}
      tabIndex={tabStop ? 0 : -1}
      onFocus={(event) => event.target === event.currentTarget && onFocus()}
      className={cx("space-y-2 rounded-control focus-visible:outline-2 focus-visible:outline-accent", node.depth > 0 && "border-l border-border pl-4")}
      data-spec-entry={node.key}
      data-editing={editing ? "" : undefined}
    >
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        {selection && selectable && <EntryPick entry={entry} selection={selection} onPick={onPick} />}
        {selection && section && <SectionPick entry={entry} below={below} selection={selection} />}
        <Heading id={`spec-${node.id}`} className={cx("font-semibold text-ink", section ? "text-lg" : "text-base")}>
          <span className="font-mono text-ink-muted" data-spec-outline>
            {node.outline}
          </span>{" "}
          {titleDiff ? <DiffTitle blocks={titleDiff} /> : node.title}
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
        {entry.change && <ChangeTag change={entry.change} />}
        {!section && <StatusMenu entry={entry} onMoved={onMoved} />}
        {entry.suspectLinks > 0 && <SuspectBadge count={entry.suspectLinks} />}
        {entry.lock && (
          <Tag title={lockSentence(node.key, entry.lock)} data-spec-lock={entry.lock.by}>
            <Icon.Lock className="mr-1 size-3" aria-hidden="true" />
            {t.control.lockedShort}
            <span className="sr-only">: {lockSentence(node.key, entry.lock)}</span>
          </Tag>
        )}
        {entry.reuse && (
          <Tag data-spec-reuse={entry.reuse.behind ? "behind" : entry.reuse.state}>
            {entry.reuse.behind ? t.reuse.badgeBehind : entry.reuse.state === "variant" ? t.reuse.badgeVariant : t.reuse.badgeFollowing}
          </Tag>
        )}
        {section && (
          <ExportMenu
            size="sm"
            title={node.title}
            projectKey={project.key}
            href={(format) => projectDocumentHref(project.key, format, node.key)}
            reqifHref={reqifExportHref(project.key, true, node.key)}
          />
        )}
        {section && <OpenSessionButton project={project} scope={{ kind: "section", rootKey: node.key }} size="sm" />}
        {entry.can.edit && !editing && (
          <Button variant="ghost" size="sm" onClick={() => onAsk({ kind: "edit", key: node.key })} data-action="edit-entry">
            {t.requirements.edit}
          </Button>
        )}
        {mayWrite && (
          <Menu
            label={t.tree.addFor(node.key)}
            trigger={(props) => (
              <Button
                variant="ghost"
                size="sm"
                icon={<Icon.Plus />}
                onClick={props.toggle}
                aria-haspopup={props["aria-haspopup"]}
                aria-expanded={props["aria-expanded"]}
                aria-controls={props["aria-controls"]}
                aria-label={t.tree.addFor(node.key)}
                data-action="add-entry"
              >
                {t.tree.add}
              </Button>
            )}
            items={[
              { label: t.tree.addAfter, onSelect: () => onAsk({ kind: "add", key: node.key, where: "after" }), attrs: { "data-action": "add-after" } },
              { label: t.tree.addBelowFirst, onSelect: () => onAsk({ kind: "add", key: node.key, where: "below" }), attrs: { "data-action": "add-below" } },
            ]}
          />
        )}
      </div>
      {editing ? (
        <RequirementEdit
          key={node.id}
          project={project}
          requirement={editableOf(entry)}
          autoFocus
          onDirty={onDirty}
          onDone={(notice) => onSaved(t.tree.saved(node.key, notice))}
          onCancel={() => onAsk(null)}
          onConflict={onConflict}
        />
      ) : collapsed ? null : entry.change?.comparison ? (
        <EntryChanges projectKey={project.key} entry={entry} change={entry.change} />
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
      {!collapsed && !editing && entry.review && <EntryReview project={project} entry={entry} onDecided={onDecided} />}
      {!collapsed && !editing && !section && <EntryComments entry={entry} />}
      {changedBelow.length > 0 && <AskForReviews projectKey={project.key} entries={changedBelow} label={t.tree.askChangedHere} onDone={onMoved} />}
      {addingBelow && (
        <AddedHere project={project} place={addedPlace(node, "below")} where={t.tree.addingBelow(node.key)} onDirty={onDirty} onAsk={onAsk} onAdded={onAdded} />
      )}
    </section>
  );
}

/** A new requirement or section written where it will stand in the document. */
function AddedHere({
  project,
  place,
  where,
  onDirty,
  onAsk,
  onAdded,
}: {
  project: Project;
  place: Place;
  where: string;
  onDirty: (dirty: boolean) => void;
  onAsk: (next: Open | null) => void;
  onAdded: (key: string) => void;
}) {
  return (
    <Card className="space-y-3 p-4" data-spec-adding="">
      <p className="text-sm font-medium text-ink">{where}</p>
      <RequirementCreate project={project} place={place} onDirty={onDirty} onCreated={(made) => onAdded(made.key)} onCancel={() => onAsk(null)} />
    </Card>
  );
}
