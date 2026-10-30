import { useEffect, useState } from "react";
import {
  VIEWER_ROLE,
  grantSubject,
  permissionWords,
  useAssignments,
  useGrantRole,
  useRevokeRole,
  useRoles,
  type Assignment,
  type RoleDescription,
} from "@/api/access";
import { useMe } from "@/api/auth";
import type { Subject } from "@/api/permissions";
import { useProjects } from "@/api/projects";
import {
  Button,
  Card,
  clampOffset,
  EmptyState,
  ErrorBanner,
  ListSearch,
  Pager,
  Select,
  SelectInput,
  shownOf,
  Skeleton,
  Table,
  Tag,
  Td,
  Th,
  useToast,
} from "@/components/ui";
import { ASSIGNMENT_PAGE_SIZE } from "@/config";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { SubjectGlyph, SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";

/**
 * Who holds what. A grant is a role, a scope and a subject: a person or a
 * group, across the whole installation or in one project. With a project, the
 * table is that project's alone and grants there; without, it is the Roles tab.
 * Read-only leaves out granting and revoking, as in an archived project.
 */
export function RoleTable({ projectKey, readOnly = false }: { projectKey?: string; readOnly?: boolean }) {
  const [q, setQ] = useState("");
  const [role, setRole] = useState("");
  const [offset, setOffset] = useState(0);
  const { data, isLoading, error } = useAssignments({ project: projectKey, q, role, offset });
  const { data: roles } = useRoles();
  const revoke = useRevokeRole();
  const confirm = useConfirm();
  const toast = useToast();
  const grants = data?.assignments ?? [];
  const narrowed = Boolean(q || role);
  const failure = (error ?? revoke.error) as Error | null;

  // Grants leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) setOffset(clampOffset(offset, data.total, ASSIGNMENT_PAGE_SIZE));
  }, [data, offset]);

  async function take(grant: Assignment) {
    const who = grantSubject(grant);
    const body = projectKey ? t.permissions.revokeBody(grant.roleName, who) : t.accessAdmin.grants.revokeBody(grant.roleName, who);
    if (await confirm({ noun: t.kit.confirm.things.role, verb: t.kit.confirm.verbs.revoke, body })) {
      revoke.mutate(grant.id, {
        onSuccess: () => toast.success(projectKey ? t.permissions.revoked(grant.roleName, who) : t.accessAdmin.grants.revoked(grant.roleName, who)),
      });
    }
  }

  return (
    <div className="space-y-4" data-role-table={projectKey ?? ""}>
      {!readOnly && <GrantForm projectKey={projectKey} roles={roles ?? []} />}
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <div className="flex flex-wrap items-center gap-3">
        <ListSearch
          aria-label={t.accessAdmin.grants.find}
          placeholder={t.accessAdmin.grants.findPlaceholder}
          value={q}
          onChange={(text) => {
            setQ(text);
            setOffset(0);
          }}
          className="w-72 max-w-full"
          data-assignment-search=""
        />
        <SelectInput
          aria-label={t.accessAdmin.grants.onlyRole}
          controlSize="sm"
          value={role}
          onChange={(event) => {
            setRole(event.target.value);
            setOffset(0);
          }}
          data-assignment-role=""
        >
          <option value="">{t.accessAdmin.grants.everyRole}</option>
          {(roles ?? [])
            .filter((each) => !projectKey || !each.orgWideOnly)
            .map((each) => (
              <option key={each.role} value={each.role}>
                {each.name}
              </option>
            ))}
        </SelectInput>
        {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, grants.length, t.kit.counted.grant)}</span>}
      </div>
      {isLoading ? (
        <Skeleton lines={4} />
      ) : grants.length === 0 ? (
        <EmptyState title={narrowed ? t.accessAdmin.grants.noMatch : projectKey ? t.permissions.empty : t.accessAdmin.grants.empty} />
      ) : (
        <Table dense aria-label={projectKey ? t.permissions.tableLabel : t.accessAdmin.tabs.roles} data-testid="role-assignments">
          <thead>
            <tr>
              <Th>{t.accessAdmin.grants.who}</Th>
              <Th>{t.accessAdmin.grants.role}</Th>
              {!projectKey && <Th>{t.accessAdmin.grants.where}</Th>}
              {!readOnly && (
                <Th>
                  <span className="sr-only">{t.accessAdmin.grants.revoke}</span>
                </Th>
              )}
            </tr>
          </thead>
          <tbody>
            {grants.map((grant) => (
              <GrantRow key={grant.id} grant={grant} showWhere={!projectKey} onRevoke={readOnly ? undefined : () => void take(grant)} />
            ))}
          </tbody>
        </Table>
      )}
      {data && <Pager label={t.accessAdmin.grants.pages} total={data.total} offset={data.offset} pageSize={ASSIGNMENT_PAGE_SIZE} onChange={setOffset} />}
    </div>
  );
}

function GrantRow({ grant, showWhere, onRevoke }: { grant: Assignment; showWhere: boolean; onRevoke?: () => void }) {
  const { data: me } = useMe();
  const who = grantSubject(grant);
  const self = grant.userId !== undefined && grant.userId === me?.user.id;
  return (
    <tr data-assignment={grant.id} data-grant={who} data-grant-role={grant.role}>
      <Td className="min-w-48">
        <span className="flex flex-wrap items-center gap-2 text-ink">
          <SubjectGlyph type={grant.groupId ? "group" : "user"} />
          <span>
            {who}
            {self && <span className="text-ink-subtle"> ({t.permissions.you})</span>}
          </span>
          {grant.groupId && <Tag>{t.accessAdmin.grants.group}</Tag>}
          {grant.guest && <Tag>{t.accessAdmin.grants.guest}</Tag>}
        </span>
      </Td>
      <Td className="whitespace-nowrap text-ink">{grant.roleName}</Td>
      {showWhere && (
        <Td className="whitespace-nowrap text-ink-muted">
          {grant.projectKey ? `${grant.projectName} (${grant.projectKey})` : t.accessAdmin.grants.everywhere}
        </Td>
      )}
      {onRevoke && (
        <Td className="text-right">
          <Button size="sm" variant="ghost" onClick={onRevoke} aria-label={t.accessAdmin.grants.revokeWho(grant.roleName, who)} data-action="revoke-grant">
            {t.accessAdmin.grants.revoke}
          </Button>
        </Td>
      )}
    </tr>
  );
}

/** Pick somebody, pick a role, and where it holds; a role for the whole installation holds there alone. */
function GrantForm({ projectKey, roles }: { projectKey?: string; roles: RoleDescription[] }) {
  const grant = useGrantRole();
  const toast = useToast();
  const { data: projects } = useProjects(false, !projectKey);
  const offered = projectKey ? roles.filter((each) => !each.orgWideOnly) : roles;
  const [subject, setSubject] = useState<Subject | null>(null);
  const [role, setRole] = useState("");
  const [scope, setScope] = useState("");
  const chosen = offered.find((each) => each.role === role) ?? offered.find((each) => each.role === VIEWER_ROLE) ?? offered[0];
  const wholeOnly = chosen?.orgWideOnly ?? false;
  const where = projectKey ?? (wholeOnly ? "" : scope);

  function submit() {
    if (!subject?.id || !chosen) return;
    grant.mutate(
      {
        role: chosen.role,
        projectKey: where || undefined,
        userId: subject.type === "user" ? subject.id : undefined,
        groupId: subject.type === "group" ? subject.id : undefined,
      },
      {
        onSuccess: (made) => {
          const who = grantSubject(made);
          toast.success(projectKey ? t.permissions.granted(made.roleName, who) : t.accessAdmin.grants.granted(made.roleName, who));
          setSubject(null);
        },
      },
    );
  }

  return (
    <Card className="space-y-3 p-4" data-grant-form>
      {subject ? (
        <div className="flex flex-wrap items-center gap-2 text-sm text-ink" data-grant-to={subject.name}>
          <SubjectGlyph type={subject.type} />
          <span>{t.accessAdmin.grants.chosen(subject.name)}</span>
          <Button size="sm" variant="ghost" onClick={() => setSubject(null)} data-action="change-subject">
            {t.accessAdmin.grants.change}
          </Button>
        </div>
      ) : (
        <SubjectPicker label={projectKey ? t.permissions.grantLabel : t.accessAdmin.grants.whoLabel} onPick={setSubject} />
      )}
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
        <Select label={t.accessAdmin.grants.role} value={chosen?.role ?? ""} onChange={(event) => setRole(event.target.value)} data-grant-role-choice="">
          {offered.map((each) => (
            <option key={each.role} value={each.role}>
              {each.name}
            </option>
          ))}
        </Select>
        {projectKey ? (
          <span />
        ) : (
          <Select label={t.accessAdmin.grants.where} value={where} disabled={wholeOnly} onChange={(event) => setScope(event.target.value)} data-grant-where="">
            <option value="">{t.accessAdmin.grants.everywhere}</option>
            {(projects ?? []).map((project) => (
              <option key={project.key} value={project.key}>
                {project.name} ({project.key})
              </option>
            ))}
          </Select>
        )}
        <Button disabled={!subject?.id || !chosen} loading={grant.isPending} onClick={submit} data-action="grant-role">
          {projectKey ? t.permissions.grant : t.accessAdmin.grants.grant}
        </Button>
      </div>
      {chosen && (
        <p className="text-xs text-ink-subtle" data-role-holds={chosen.role}>
          {chosen.permissions.length > 0
            ? t.accessAdmin.grants.holds(chosen.name, chosen.permissions.map((p) => permissionWords(p)).join(", "))
            : t.accessAdmin.grants.holdsNothing(chosen.name)}
          {wholeOnly && !projectKey ? ` ${t.accessAdmin.grants.wholeOnly}` : ""}
        </p>
      )}
      {grant.error && <ErrorBanner>{grant.error.message}</ErrorBanner>}
    </Card>
  );
}
