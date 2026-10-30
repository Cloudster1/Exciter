import { useId, useState, type FormEvent, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import {
  useArmatureAccount,
  useArmatureIssueTypes,
  useArmatureProjects,
  useCreateArmatureIssues,
  type ArmatureCreateFailure,
  type ArmatureCreated,
} from "@/api/armature";
import type { Project } from "@/api/projects";
import { Button, Checkbox, Dialog, ErrorBanner, IconButton, Select, DialogFooter } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_ACCOUNT_PATH, ARMATURE_CREATE_MAX, ARMATURE_CREATE_RELATIONS } from "@/config";
import { t } from "@/i18n";
import { issueUrl } from "./ArmatureLinks";

// Issues made from requirements (#28), after Stator's create dialog: requirements are picked in the specification
// view or the grid, and each becomes an issue traced to it.

/** A requirement picked for an issue of its own. */
export interface Picked {
  key: string;
  title: string;
  outline: string;
}

/** Orders requirements as the project's outline does, so issues are filed in the order the document reads. */
export function byOutline(a: Picked, b: Picked): number {
  const left = a.outline.split(".").map(Number);
  const right = b.outline.split(".").map(Number);
  for (let i = 0; i < Math.max(left.length, right.length); i++) {
    const diff = (left[i] ?? -1) - (right[i] ?? -1);
    if (diff !== 0) return diff;
  }
  return 0;
}

export interface Selection {
  picked: Picked[];
  has: (key: string) => boolean;
  set: (items: Picked[], on: boolean) => void;
  clear: () => void;
}

/** The requirements picked on a page, in outline order. */
export function useSelection(): Selection {
  const [picked, setPicked] = useState<Picked[]>([]);
  return {
    picked,
    has: (key) => picked.some((each) => each.key === key),
    set: (items, on) =>
      setPicked((now) => {
        const rest = now.filter((each) => !items.some((item) => item.key === each.key));
        return on ? [...rest, ...items].sort(byOutline) : rest;
      }),
    clear: () => setPicked([]),
  };
}

/** Whether the reader may pick a project's requirements for Armature: it is connected, and they edit the project. */
export function useMayCreateIssues(project: Project): boolean {
  const account = useArmatureAccount();
  return Boolean(account.data?.configured && project.can.edit && !project.archivedAt);
}

/** A checkbox picking one requirement, named for the reader of a screen. */
export function PickBox({ selection, item }: { selection: Selection; item: Picked }) {
  return (
    <Checkbox
      label={<span className="sr-only">{t.armature.create.select(item.key)}</span>}
      className="shrink-0"
      checked={selection.has(item.key)}
      onChange={(event) => selection.set([item], event.target.checked)}
      data-pick={item.key}
    />
  );
}

/** What is picked, the way on to Armature where the reader may take it, and what else the page offers for the
 * picked; it stays below the page while anything is picked. */
export function SelectionBar({
  projectKey,
  selection,
  armature = true,
  children,
}: {
  projectKey: string;
  selection: Selection;
  armature?: boolean;
  children?: ReactNode;
}) {
  const c = t.armature.create;
  const [open, setOpen] = useState(false);
  const count = selection.picked.length;
  if (count === 0) return null;
  const tooMany = armature && count > ARMATURE_CREATE_MAX;
  return (
    <div
      className="sticky bottom-4 z-10 mt-4 flex flex-wrap items-center gap-3 rounded-overlay border border-border bg-surface-overlay p-3 shadow-2"
      data-selection-bar
    >
      <span role="status" className="text-sm text-ink">
        {c.selected(count)}
      </span>
      {tooMany && <span className="text-sm text-danger">{c.tooMany(ARMATURE_CREATE_MAX)}</span>}
      <div className="ml-auto flex gap-2">
        <Button variant="ghost" size="sm" onClick={selection.clear} data-action="clear-selection">
          {c.clear}
        </Button>
        {armature && (
          <Button size="sm" icon={<Icon.Plus />} disabled={tooMany} onClick={() => setOpen(true)} data-action="create-armature-issues">
            {c.open}
          </Button>
        )}
      </div>
      {children}
      {open && (
        <CreateIssuesDialog
          projectKey={projectKey}
          requirements={selection.picked}
          onClose={(made) => {
            setOpen(false);
            if (made) selection.clear();
          }}
        />
      )}
    </div>
  );
}

/** What to tell the author when the whole round was refused: Armature's sentences on its fields say the most. */
export function refusalText(error: unknown): string | null {
  if (!error) return null;
  if (!(error instanceof ApiError)) return t.armature.create.unreachable;
  const fields = Object.values(error.fields);
  return fields.length > 0 ? fields.join(" ") : error.message;
}

interface Answer {
  created: ArmatureCreated[];
  failed: ArmatureCreateFailure | null;
}

/** Files one Armature issue per requirement in a project, type and relation the author picks, then says what became of each. */
export function CreateIssuesDialog({ projectKey, requirements, onClose }: { projectKey: string; requirements: Picked[]; onClose: (made: boolean) => void }) {
  const formId = useId();
  const c = t.armature.create;
  const id = useId();
  const projects = useArmatureProjects(true);
  const status = projects.data?.status;
  const types = useArmatureIssueTypes(status === "ok");
  const create = useCreateArmatureIssues(projectKey);
  const [kept, setKept] = useState(requirements);
  const [project, setProject] = useState("");
  const [typeId, setTypeId] = useState("");
  const [relation, setRelation] = useState<(typeof ARMATURE_CREATE_RELATIONS)[number]>("implements");
  const [answer, setAnswer] = useState<Answer | null>(null);

  const creatable = status === "ok" ? projects.data!.projects.filter((each) => each.canCreate) : [];
  const chosen = creatable.some((each) => each.key === project) ? project : (creatable[0]?.key ?? "");
  const ready = Boolean(chosen) && kept.length > 0 && !create.isPending;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!ready) return;
    create.mutate(
      { requirementKeys: kept.map((each) => each.key), armatureProject: chosen, relation, ...(typeId ? { typeId } : {}) },
      { onSuccess: setAnswer },
    );
  }

  const close = () => onClose(Boolean(answer && answer.created.length > 0));

  return (
    <Dialog open title={c.title(kept.length)} onClose={close} size="lg" attrs={{ "data-create-issues-dialog": "" }}>
      {answer ? (
        <Outcome requirements={kept} answer={answer} onClose={close} />
      ) : (
        <form id={formId} onSubmit={submit} className="space-y-4" noValidate>
          <p className="text-sm text-ink-muted">{c.intro}</p>
          {projects.isPending && (
            <p role="status" className="text-sm text-ink-muted">
              {c.loading}
            </p>
          )}
          {status === "not_configured" && <p className="text-sm text-ink-muted">{t.armature.account.notConfigured}</p>}
          {status === "not_connected" && (
            <p className="text-sm text-ink-muted" data-armature-connect>
              {c.connect}{" "}
              <a href={ARMATURE_ACCOUNT_PATH} className="underline">
                {c.connectLink}
              </a>
            </p>
          )}
          {status === "rejected" && <ErrorBanner>{c.rejected}</ErrorBanner>}
          {(status === "unreachable" || projects.isError) && <ErrorBanner>{c.unreachable}</ErrorBanner>}
          {status === "ok" && creatable.length === 0 && <ErrorBanner>{c.noProjects}</ErrorBanner>}
          {creatable.length > 0 && (
            <div className="grid gap-3 sm:grid-cols-3">
              <Select label={c.project} id={`${id}-project`} value={chosen} onChange={(e) => setProject(e.target.value)} data-field="armature-project">
                {creatable.map((each) => (
                  <option key={each.key} value={each.key}>
                    {c.projectOption(each.key, each.name)}
                  </option>
                ))}
              </Select>
              <Select label={c.type} id={`${id}-type`} value={typeId} onChange={(e) => setTypeId(e.target.value)} data-field="issue-type">
                <option value="">{c.defaultType}</option>
                {(types.data?.status === "ok" ? types.data.issueTypes : []).map((each) => (
                  <option key={each.id} value={each.id}>
                    {each.name}
                  </option>
                ))}
              </Select>
              <Select
                label={c.relation}
                id={`${id}-relation`}
                value={relation}
                onChange={(e) => setRelation(e.target.value as typeof relation)}
                data-field="relation"
              >
                {ARMATURE_CREATE_RELATIONS.map((each) => (
                  <option key={each} value={each}>
                    {c.relations[each]}
                  </option>
                ))}
              </Select>
            </div>
          )}
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-ink-muted">{c.requirements}</legend>
            <ul className="space-y-1" data-create-requirements>
              {kept.map((each) => (
                <li key={each.key} className="flex items-center gap-2 text-sm" data-create-requirement={each.key}>
                  <span className="font-mono text-xs text-ink-muted">{each.key}</span>
                  <span className="min-w-0 flex-1 truncate text-ink">{each.title}</span>
                  {kept.length > 1 && (
                    <IconButton
                      icon={<Icon.X className="size-3" />}
                      label={c.leaveOut(each.key)}
                      size="xs"
                      onClick={() => setKept((now) => now.filter((other) => other.key !== each.key))}
                    />
                  )}
                </li>
              ))}
            </ul>
          </fieldset>
          {create.error && <ErrorBanner>{refusalText(create.error)}</ErrorBanner>}
          {create.isPending && (
            <p role="status" className="text-sm text-ink-muted">
              {c.creating}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="secondary" onClick={() => onClose(false)}>
              {c.cancel}
            </Button>
            <Button type="submit" form={formId} disabled={!ready} loading={create.isPending} data-action="create-issues">
              {c.submit(kept.length)}
            </Button>
          </DialogFooter>
        </form>
      )}
    </Dialog>
  );
}

/** Each requirement's new issue, the one Armature refused, and the ones not tried after it. */
function Outcome({ requirements, answer, onClose }: { requirements: Picked[]; answer: Answer; onClose: () => void }) {
  const c = t.armature.create;
  const account = useArmatureAccount();
  const baseUrl = account.data?.baseUrl ?? "";
  return (
    <div className="space-y-3">
      <p role="status" className="text-sm text-ink">
        {c.done(answer.created.length, requirements.length)}
      </p>
      <ul className="space-y-1 text-sm" data-create-results>
        {requirements.map((each) => {
          const made = answer.created.find((one) => one.requirementKey === each.key);
          const failed = answer.failed?.requirementKey === each.key ? answer.failed : null;
          return (
            <li key={each.key} className="flex items-start gap-2" data-result={made ? "created" : failed ? "failed" : "skipped"} data-requirement={each.key}>
              {made ? (
                <>
                  <Icon.Check className="mt-0.5 shrink-0 text-success" aria-hidden="true" />
                  <span>
                    <span className="font-mono text-xs text-ink-muted">{each.key}</span>{" "}
                    <a href={made.issue.url || issueUrl(baseUrl, made.issue.key)} className="font-medium underline" data-created-key={made.issue.key}>
                      {made.issue.key}
                    </a>{" "}
                    {made.issue.summary}
                  </span>
                </>
              ) : failed ? (
                <>
                  <Icon.Warning className="mt-0.5 shrink-0 text-danger" aria-hidden="true" />
                  <span>
                    <span className="font-mono text-xs text-ink-muted">{each.key}</span> {failed.message}
                    {failed.code === "armature_unreachable" && ` ${c.maybeMade}`}
                  </span>
                </>
              ) : (
                <>
                  <span aria-hidden="true" className="w-4 shrink-0" />
                  <span className="text-ink-muted">
                    <span className="font-mono text-xs">{each.key}</span> {c.skipped}
                  </span>
                </>
              )}
            </li>
          );
        })}
      </ul>
      <DialogFooter>
        <Button type="button" onClick={onClose} data-action="close-created">
          {c.close}
        </Button>
      </DialogFooter>
    </div>
  );
}
