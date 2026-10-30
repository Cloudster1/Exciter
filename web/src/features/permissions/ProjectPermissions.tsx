import { useState } from "react";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { ROLES, subjectKey, useProjectGrants, useSetProjectGrants, type Grant, type Role } from "@/api/permissions";
import type { Project } from "@/api/projects";
import { Button, Card, ErrorBanner, IconButton, PageHeader, SelectInput, Skeleton, Table, Td, Th, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { AccessDialog } from "./AccessDialog";
import { SubjectGlyph, SubjectPicker } from "./SubjectPicker";

/** The role somebody new to the table starts with: the narrowest, so adding a row never opens more than it says. */
const FIRST_ROLE: Role = "viewer";

const shape = (grants: Grant[]) => JSON.stringify(grants.map((grant) => [subjectKey(grant.subject), grant.role]));

/** Who holds which role in a project, changed as a whole and saved at once; its administrators' page. */
export function ProjectPermissions({ project }: { project: Project }) {
  const allowed = project.can.administer;
  const { data, error, refetch } = useProjectGrants(project.key, allowed);
  return (
    <Page width="narrow" className="space-y-6" data-project-permissions={project.key}>
      <PageHeader title={t.permissions.title} meta={t.projects.projectAndKey(project.name, project.key)} />
      {!allowed ? (
        <p className="text-sm text-ink-muted">{t.permissions.notProjectAdmin}</p>
      ) : error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : (
        <Grants project={project} saved={data} />
      )}
    </Page>
  );
}

function Grants({ project, saved }: { project: Project; saved: Grant[] }) {
  const { data: me } = useMe();
  const save = useSetProjectGrants(project.key);
  const [grants, setGrants] = useState<Grant[]>(saved);
  const [notice, setNotice] = useState("");
  const [inspecting, setInspecting] = useState(false);
  const archived = Boolean(project.archivedAt);
  const dirty = shape(grants) !== shape(saved);
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const general = save.error && Object.keys(fields).length === 0;

  const change = (next: Grant[]) => {
    setNotice("");
    save.reset();
    setGrants(next);
  };

  return (
    <>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.permissions.intro}</p>
        {archived && <p className="text-sm text-ink-muted">{t.permissions.archived}</p>}
        {grants.length === 0 ? (
          <p className="text-sm text-ink-subtle">{t.permissions.empty}</p>
        ) : (
          <Table dense aria-label={t.permissions.tableLabel}>
            <thead>
              <tr>
                <Th>{t.permissions.columnWho}</Th>
                <Th>{t.permissions.columnRole}</Th>
                <Th>
                  <span className="sr-only">{t.permissions.columnRemove}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {grants.map((grant, index) => {
                const self = grant.subject.type === "user" && grant.subject.id === me?.user.id;
                const problem = fields[`grants.${index}.role`] ?? fields[`grants.${index}.subject`];
                return (
                  <tr key={subjectKey(grant.subject)} data-grant={grant.subject.name} data-grant-type={grant.subject.type}>
                    <Td>
                      <span className="flex items-center gap-2 text-ink">
                        <SubjectGlyph type={grant.subject.type} />
                        {self ? t.permissions.yourself(grant.subject.name) : grant.subject.name}
                      </span>
                      {problem && (
                        <span className="mt-1 block text-xs text-danger" data-grant-problem>
                          {problem}
                        </span>
                      )}
                    </Td>
                    <Td>
                      <SelectInput
                        controlSize="sm"
                        value={grant.role}
                        disabled={archived || save.isPending}
                        invalid={Boolean(problem)}
                        aria-label={t.permissions.roleOf(grant.subject.name)}
                        onChange={(event) => change(grants.map((each, i) => (i === index ? { ...each, role: event.target.value as Role } : each)))}
                        data-role-select
                      >
                        {ROLES.map((role) => (
                          <option key={role} value={role} disabled={role === "admin" && grant.subject.type === "everyone"}>
                            {t.permissions.roles[role]}
                          </option>
                        ))}
                      </SelectInput>
                    </Td>
                    <Td className="text-right">
                      {!archived && (
                        <IconButton
                          icon={<Icon.X />}
                          label={t.permissions.remove(grant.subject.name)}
                          size="sm"
                          onClick={() => change(grants.filter((_, i) => i !== index))}
                          data-action="remove-grant"
                        />
                      )}
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
        {!archived && (
          <SubjectPicker
            label={t.permissions.pickerLabel}
            allowEveryone
            exclude={grants.map((grant) => subjectKey(grant.subject))}
            onPick={(subject) => {
              change([...grants, { subject, role: FIRST_ROLE }]);
              setNotice(t.permissions.added(subject.name));
            }}
          />
        )}
        {general && <ErrorBanner>{save.error.message}</ErrorBanner>}
        {!archived && (
          <div className="flex flex-wrap items-center gap-2">
            <Button
              disabled={!dirty}
              loading={save.isPending}
              onClick={() =>
                save.mutate(grants, {
                  // The server answers the table in its own order, which the form then shows.
                  onSuccess: (stored) => {
                    setGrants(stored);
                    setNotice(t.permissions.saved);
                  },
                })
              }
              data-action="save-grants"
            >
              {t.permissions.save}
            </Button>
            {dirty && (
              <Button variant="secondary" onClick={() => change(saved)} data-action="discard-grants">
                {t.permissions.discard}
              </Button>
            )}
            <span role="status" className="text-sm text-ink-muted">
              {notice || (dirty ? t.permissions.unsaved : "")}
            </span>
          </div>
        )}
      </Card>
      <Card className="space-y-3 p-4">
        <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]" data-role-legend>
          {ROLES.map((role) => (
            <div key={role} className="contents">
              <dt className="font-medium text-ink">{t.permissions.roles[role]}</dt>
              <dd className="text-ink-muted">{t.permissions.roleHints[role]}</dd>
            </div>
          ))}
        </dl>
        <p className="text-sm text-ink-muted">{t.permissions.orgAdminsNote}</p>
        <Button variant="secondary" icon={<Icon.Shield />} onClick={() => setInspecting(true)} data-action="check-access">
          {t.permissions.checkAccess}
        </Button>
      </Card>
      {inspecting && <AccessDialog project={project} onClose={() => setInspecting(false)} />}
    </>
  );
}
