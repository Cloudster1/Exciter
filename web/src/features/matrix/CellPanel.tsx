import { useRef, useState, type CSSProperties, type FormEvent, type RefObject } from "react";
import { useArmatureLinkActions, useArmatureLinks, type ArmatureLink, type ArmatureRelation } from "@/api/armature";
import type { Matrix } from "@/api/coverage";
import { linkWays, useLinkActions, useLinks, useProjectLinkTypes, type Direction, type TraceLink } from "@/api/links";
import { Button, ErrorBanner, IconButton, Select, Skeleton } from "@/components/ui";
import { useEscape, useFocusReturn, useOutsidePress } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { checked } from "@/features/links/LinksPanel";
import { ClearDialog, ClearLinkDialog, SuspectBadge } from "@/features/links/Suspect";
import { t } from "@/i18n";
import { columnName, ISSUE_RELATIONS } from "./address";

type Row = Matrix["rows"][number];
type Column = Matrix["columns"][number];

const capitalized = (phrase: string) => phrase.charAt(0).toUpperCase() + phrase.slice(1);

/** A cell's links in a panel by the cell: whoever may link its row removes them, clears a suspect one, and adds one
 * (#149); anybody else reads them. */
export function CellPanel({
  projectKey,
  row,
  column,
  linkIds,
  anchor,
  place,
  panelRef,
  onClose,
}: {
  projectKey: string;
  row: Row;
  column: Column;
  linkIds: string[];
  anchor: RefObject<HTMLElement | null>;
  place: CSSProperties;
  panelRef: RefObject<HTMLDivElement | null>;
  onClose: () => void;
}) {
  // A clearing dialog sits outside the panel, so pressing in it or Escape there must not close the panel under it.
  const [dialog, setDialog] = useState(false);
  const outside = useRef([anchor, panelRef]);
  useEscape(!dialog, onClose);
  useOutsidePress(!dialog, outside.current, onClose);
  useFocusReturn(true, panelRef, false);
  const [notice, setNotice] = useState("");
  const name = columnName(column);
  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-label={t.matrix.cellTitle(row.key, name)}
      style={place}
      className="fixed z-30 w-[min(24rem,calc(100vw-1rem))] space-y-3 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-3 text-sm shadow-2"
      data-cell-panel={`${row.key}:${name}`}
    >
      <div className="flex items-center justify-between gap-2">
        <h2 className="font-medium text-ink">{t.matrix.cellTitle(row.key, name)}</h2>
        <IconButton icon={<Icon.X />} label={t.matrix.close} size="sm" onClick={onClose} />
      </div>
      {column.requirement ? (
        <RequirementCell projectKey={projectKey} row={row} otherKey={column.requirement.key} linkIds={linkIds} onDialog={setDialog} onNotice={setNotice} />
      ) : (
        <IssueCell row={row} issueKey={column.issueKey} linkIds={linkIds} onDialog={setDialog} onNotice={setNotice} />
      )}
      {!row.canLink && <p className="text-xs text-ink-subtle">{t.matrix.readOnly}</p>}
      <p role="status" className="sr-only" data-cell-notice>
        {notice}
      </p>
    </div>
  );
}

interface CellProps {
  row: Row;
  linkIds: string[];
  onDialog: (open: boolean) => void;
  onNotice: (notice: string) => void;
}

/** The trace links between a row and a requirement column, read from the row's links for their revisions. */
function RequirementCell({ projectKey, row, otherKey, linkIds, onDialog, onNotice }: CellProps & { projectKey: string; otherKey: string }) {
  const { data: all, error } = useLinks(row.key);
  const { data: types = [] } = useProjectLinkTypes(projectKey);
  const actions = useLinkActions(row.key);
  const [checking, setChecking] = useState<TraceLink | null>(null);
  const [choice, setChoice] = useState("");
  const ways = linkWays(types, t.links.reads);
  const picked = choice || ways[0]?.value || "";
  const links = (all ?? []).filter((link) => linkIds.includes(link.id));
  const check = (link: TraceLink | null) => {
    setChecking(link);
    onDialog(link !== null);
  };
  function add(event: FormEvent) {
    event.preventDefault();
    const [typeId, direction] = picked.split(":") as [string, Direction];
    actions.create.mutate({ typeId, direction, otherKey }, { onSuccess: () => onNotice(t.matrix.linked(otherKey)) });
  }
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  if (!all) return <Skeleton />;
  return (
    <>
      <LinkList
        items={links.map((link) => ({
          id: link.id,
          label: `${capitalized(link.phrase)} ${link.other.key}`,
          suspect: link.suspect.length > 0,
          removeLabel: t.links.remove(link.phrase, link.other.key),
          checkLabel: t.suspect.checkLabel(link.other.key),
          onCheck: () => check(link),
          onRemove: () => actions.remove.mutate(link, { onSuccess: () => onNotice(t.matrix.unlinked(otherKey)) }),
        }))}
        mayChange={row.canLink}
      />
      {(actions.create.error ?? actions.remove.error) && <ErrorBanner>{(actions.create.error ?? actions.remove.error)?.message}</ErrorBanner>}
      {row.canLink && ways.length > 0 && (
        <form onSubmit={add} className="flex flex-wrap items-end gap-2 border-t border-border pt-3" aria-label={t.matrix.linkThem} data-cell-add>
          <div className="min-w-0 flex-1">
            <Select label={t.links.linkType} value={picked} onChange={(event) => setChoice(event.target.value)} controlSize="sm">
              {ways.map((way) => (
                <option key={way.value} value={way.value}>
                  {way.label}
                </option>
              ))}
            </Select>
          </div>
          <Button type="submit" variant="secondary" size="sm" icon={<Icon.Link />} loading={actions.create.isPending} data-action="cell-link">
            {t.matrix.linkThem}
          </Button>
        </form>
      )}
      {checking && (
        <ClearLinkDialog
          link={checked(checking, { key: row.key, revision: row.revision })}
          onCleared={() => onNotice(t.suspect.cleared(checking.other.key))}
          onClose={() => check(null)}
        />
      )}
    </>
  );
}

/** The traces between a row and an Armature issue column. */
function IssueCell({ row, issueKey, linkIds, onDialog, onNotice }: CellProps & { issueKey: string }) {
  const { data: all, error } = useArmatureLinks(row.key, true);
  const { create, remove, clear } = useArmatureLinkActions(row.key);
  const [checking, setChecking] = useState<ArmatureLink | null>(null);
  const [relation, setRelation] = useState<ArmatureRelation>("implements");
  const links = (all ?? []).filter((link) => linkIds.includes(link.id));
  const check = (link: ArmatureLink | null) => {
    setChecking(link);
    onDialog(link !== null);
  };
  function add(event: FormEvent) {
    event.preventDefault();
    create.mutate({ issueKey, relation }, { onSuccess: () => onNotice(t.matrix.linked(issueKey)) });
  }
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  if (!all) return <Skeleton />;
  return (
    <>
      <LinkList
        items={links.map((link) => ({
          id: link.id,
          label: `${t.matrix.relationNames[link.relation]} ${link.issueKey}`,
          suspect: link.suspect,
          removeLabel: t.armature.links.remove(link.issueKey, t.armature.links.relations[link.relation]),
          checkLabel: t.suspect.checkLabel(link.issueKey),
          onCheck: () => check(link),
          onRemove: () => remove.mutate(link, { onSuccess: () => onNotice(t.matrix.unlinked(issueKey)) }),
        }))}
        mayChange={row.canLink}
      />
      {(create.error ?? remove.error) && <ErrorBanner>{(create.error ?? remove.error)?.message}</ErrorBanner>}
      {row.canLink && (
        <form onSubmit={add} className="flex flex-wrap items-end gap-2 border-t border-border pt-3" aria-label={t.matrix.linkThem} data-cell-add>
          <div className="min-w-0 flex-1">
            <Select label={t.matrix.relation} value={relation} onChange={(event) => setRelation(event.target.value as ArmatureRelation)} controlSize="sm">
              {ISSUE_RELATIONS.map((each) => (
                <option key={each} value={each}>
                  {t.matrix.relationNames[each]}
                </option>
              ))}
            </Select>
          </div>
          <Button type="submit" variant="secondary" size="sm" icon={<Icon.Link />} loading={create.isPending} data-action="cell-link">
            {t.matrix.linkThem}
          </Button>
        </form>
      )}
      {checking && (
        <ClearDialog
          title={t.suspect.armatureTitle(row.key, checking.issueKey)}
          intro={t.suspect.armatureIntro}
          changes={[{ reqKey: row.key, from: checking.reviewedRevision, to: row.revision }]}
          pending={clear.isPending}
          error={clear.error}
          onClose={() => check(null)}
          onConfirm={(comment) =>
            clear.mutate(
              { id: checking.id, revision: row.revision, comment },
              {
                onSuccess: () => {
                  onNotice(t.suspect.armatureCleared(checking.issueKey));
                  check(null);
                },
              },
            )
          }
        />
      )}
    </>
  );
}

interface Item {
  id: string;
  label: string;
  suspect: boolean;
  removeLabel: string;
  checkLabel: string;
  onCheck: () => void;
  onRemove: () => void;
}

/** A cell's links, each with its suspect mark and, for whoever may change them, checking and removing. */
function LinkList({ items, mayChange }: { items: Item[]; mayChange: boolean }) {
  if (items.length === 0) return <p className="text-ink-subtle">{t.matrix.noLinkHere}</p>;
  return (
    <ul className="space-y-1">
      {items.map((item) => (
        <li key={item.id} className="flex flex-wrap items-center gap-2" data-cell-link={item.id}>
          <span className="text-ink">{item.label}</span>
          {item.suspect && <SuspectBadge />}
          {item.suspect && mayChange && (
            <Button variant="link" onClick={item.onCheck} aria-label={item.checkLabel} data-action="check-link">
              {t.suspect.check}
            </Button>
          )}
          {mayChange && (
            <IconButton icon={<Icon.X className="size-3" />} label={item.removeLabel} size="xs" onClick={item.onRemove} data-action="remove-link" />
          )}
        </li>
      ))}
    </ul>
  );
}
