import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useBaselines } from "@/api/baselines";
import { ApiError } from "@/api/client";
import { subjectKey, type Subject } from "@/api/permissions";
import type { Project } from "@/api/projects";
import { useSessionActions, type SessionInput, type SessionScope } from "@/api/sessions";
import { useTree } from "@/api/tree";
import { Button, Dialog, ErrorBanner, Field, IconButton, Select, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SESSION_TITLE_MAX_LENGTH } from "@/config";
import { SubjectGlyph, SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";

type Role = "approver" | "reviewer";
type Picked = Subject & { role: Role };
type Since = "" | "approved" | "baseline";

/** Opens a review session over a section, a baseline or a query, with who takes part, a deadline and a quorum (#204). */
export function OpenSessionDialog({ project, scope: initial, onClose }: { project: Project; scope?: Partial<SessionScope>; onClose: () => void }) {
  const { open } = useSessionActions(project.key);
  const navigate = useNavigate();
  const { data: tree } = useTree(project.key);
  const { data: baselines } = useBaselines(project.key);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<SessionScope["kind"]>(initial?.kind ?? "section");
  const [rootKey, setRootKey] = useState(initial?.rootKey ?? "");
  const [baselineId, setBaselineId] = useState(initial?.baselineId ?? "");
  const [query, setQuery] = useState(initial?.query ?? "");
  const [since, setSince] = useState<Since>("");
  const [sinceBaseline, setSinceBaseline] = useState("");
  const [people, setPeople] = useState<Picked[]>([]);
  const [deadline, setDeadline] = useState("");
  const [quorum, setQuorum] = useState("100");
  const [problem, setProblem] = useState("");
  const fields = open.error instanceof ApiError ? open.error.fields : {};
  const sections = (tree ?? []).filter((node) => node.kind === "section");

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!people.some((each) => each.role === "approver")) {
      setProblem(t.sessions.approverNeeded);
      return;
    }
    setProblem("");
    const body: SessionInput = {
      title: title.trim(),
      description: description.trim() || undefined,
      scope: kind === "section" ? { kind, rootKey } : kind === "baseline" ? { kind, baselineId } : { kind, query },
      participants: people.map((p) =>
        p.type === "user"
          ? { subjectType: "user", userId: p.id ?? undefined, role: p.role }
          : { subjectType: "group", groupId: p.id ?? undefined, role: p.role },
      ),
      quorum: Number(quorum) || undefined,
      deadline: deadline ? new Date(deadline).toISOString() : undefined,
      since: since === "approved" ? { base: "approved" } : since === "baseline" && sinceBaseline ? { base: "baseline", baselineId: sinceBaseline } : undefined,
    };
    open.mutate(body, {
      onSuccess: (made) => void navigate({ to: "/p/$projectKey/sessions/$sessionId", params: { projectKey: project.key, sessionId: made.id } }),
    });
  }

  return (
    <Dialog title={t.sessions.openTitle} onClose={onClose} wide data-open-session="">
      <form onSubmit={submit} className="space-y-3" noValidate aria-label={t.sessions.openTitle}>
        {open.error && Object.keys(fields).length === 0 && <ErrorBanner>{open.error.message}</ErrorBanner>}
        <Field
          label={t.sessions.name}
          placeholder={t.sessions.namePlaceholder}
          value={title}
          maxLength={SESSION_TITLE_MAX_LENGTH}
          error={fields.title}
          onChange={(event) => setTitle(event.target.value)}
          required
        />
        <Field
          label={t.sessions.description}
          value={description}
          rows={2}
          error={fields.description}
          onChange={(event) => setDescription(event.target.value)}
        />
        <Select label={t.sessions.scope} value={kind} onChange={(event) => setKind(event.target.value as SessionScope["kind"])} error={fields.scope}>
          {(["section", "baseline", "query"] as const).map((each) => (
            <option key={each} value={each}>
              {t.sessions.scopes[each]}
            </option>
          ))}
        </Select>
        {kind === "section" && (
          <Select label={t.sessions.section} value={rootKey} onChange={(event) => setRootKey(event.target.value)}>
            <option value="" />
            {sections.map((node) => (
              <option key={node.key} value={node.key}>
                {`${node.outline} ${node.title}`}
              </option>
            ))}
          </Select>
        )}
        {kind === "baseline" && (
          <Select label={t.sessions.baseline} value={baselineId} onChange={(event) => setBaselineId(event.target.value)}>
            <option value="" />
            {(baselines ?? []).map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </Select>
        )}
        {kind === "query" && <Field label={t.sessions.query} hint={t.sessions.queryHint} value={query} onChange={(event) => setQuery(event.target.value)} />}
        <Select label={t.sessions.since} value={since} onChange={(event) => setSince(event.target.value as Since)} error={fields.since}>
          <option value="">{t.sessions.sinceNone}</option>
          <option value="approved">{t.sessions.sinceApproved}</option>
          <option value="baseline">{t.sessions.sinceBaseline}</option>
        </Select>
        {since === "baseline" && (
          <Select label={t.sessions.sinceBaseline} value={sinceBaseline} onChange={(event) => setSinceBaseline(event.target.value)}>
            <option value="" />
            {(baselines ?? []).map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </Select>
        )}
        <fieldset className="space-y-2" aria-describedby={problem || fields.participants ? "session-participants-error" : undefined}>
          <legend className="mb-1 text-sm font-medium text-ink">{t.sessions.participants}</legend>
          {people.length === 0 ? (
            <p className="text-sm text-ink-muted">{t.sessions.participantsEmpty}</p>
          ) : (
            <ul className="space-y-1" aria-label={t.sessions.participants}>
              {people.map((p) => (
                <li key={subjectKey(p)} className="flex items-center gap-2 text-sm" data-participant={p.name}>
                  <SubjectGlyph type={p.type} />
                  <span className="min-w-0 flex-1 truncate">{p.name}</span>
                  <SelectInput
                    aria-label={t.sessions.role(p.name)}
                    value={p.role}
                    controlSize="sm"
                    className="w-auto"
                    onChange={(event) =>
                      setPeople(people.map((each) => (subjectKey(each) === subjectKey(p) ? { ...each, role: event.target.value as Role } : each)))
                    }
                  >
                    <option value="approver">{t.sessions.roles.approver}</option>
                    <option value="reviewer">{t.sessions.roles.reviewer}</option>
                  </SelectInput>
                  <IconButton
                    icon={<Icon.X />}
                    label={t.sessions.removeParticipant(p.name)}
                    onClick={() => setPeople(people.filter((each) => subjectKey(each) !== subjectKey(p)))}
                  />
                </li>
              ))}
            </ul>
          )}
          <SubjectPicker
            label={t.sessions.pickParticipant}
            inline
            exclude={people.map(subjectKey)}
            onPick={(picked) => {
              setProblem("");
              setPeople([...people, { ...picked, role: "approver" }]);
            }}
          />
          {(problem || fields.participants) && (
            <p id="session-participants-error" className="text-sm text-danger">
              {problem || fields.participants}
            </p>
          )}
        </fieldset>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field
            type="datetime-local"
            label={t.sessions.deadline}
            hint={t.sessions.deadlineHint}
            value={deadline}
            error={fields.deadline}
            onChange={(event) => setDeadline(event.target.value)}
          />
          <Field
            type="number"
            label={t.sessions.quorum}
            hint={t.sessions.quorumHint}
            min={1}
            max={100}
            value={quorum}
            error={fields.quorum}
            onChange={(event) => setQuorum(event.target.value)}
          />
        </div>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.sessions.cancel}
          </Button>
          <Button type="submit" loading={open.isPending} disabled={!title.trim()} data-action="open-session-send">
            {t.sessions.openSend}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
