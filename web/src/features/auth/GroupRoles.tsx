import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useGroupRoles, useProvider, useRemoveGroupRole, useSetGroupRole, type GrantedRole, type GroupRole } from "@/api/auth";
import { Button, Card, ErrorBanner, Field, Select, Table, Td, Th } from "@/components/ui";
import { SSO_GROUP_EXAMPLE } from "@/config";
import { t } from "@/i18n";

const GRANTED: GrantedRole[] = ["member", "admin"];

/** Which of the provider's groups grant which role, for the installation's administrators. */
export function GroupRoles() {
  const { data: view } = useProvider();
  const { data } = useGroupRoles();
  const set = useSetGroupRole();
  const remove = useRemoveGroupRole();
  const [group, setGroup] = useState("");
  const [role, setRole] = useState<GrantedRole>("member");
  const [notice, setNotice] = useState("");
  const mapping = data ?? [];
  const configured = Boolean(view?.provider);
  const fields = set.error instanceof ApiError ? set.error.fields : {};
  const failure = (set.error && Object.keys(fields).length === 0 ? set.error : null) ?? remove.error;

  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    set.mutate(
      { group, role },
      {
        onSuccess: (saved) => {
          setGroup("");
          setNotice(t.sso.mapped(saved.group, t.members.roleNames[saved.role]));
        },
      },
    );
  }

  function unmap(row: GroupRole) {
    setNotice("");
    remove.mutate({ id: row.id }, { onSuccess: () => setNotice(t.sso.unmapped(row.group)) });
  }

  return (
    <Card className="mt-4 space-y-3 p-4" data-group-roles="">
      <h2 className="text-sm font-semibold text-ink">{t.sso.groupRolesTitle}</h2>
      <p className="text-sm text-ink-muted">{t.sso.groupRolesIntro}</p>
      {!configured ? (
        <p className="text-sm text-ink-subtle">{t.sso.groupRolesNeedProvider}</p>
      ) : (
        <>
          {mapping.length === 0 ? (
            <p className="text-sm text-ink-subtle">{t.sso.groupRolesEmpty}</p>
          ) : (
            <Table dense aria-label={t.sso.groupRolesTitle}>
              <thead>
                <tr>
                  <Th>{t.sso.group}</Th>
                  <Th>{t.sso.role}</Th>
                  <Th>
                    <span className="sr-only">{t.sso.unmap}</span>
                  </Th>
                </tr>
              </thead>
              <tbody>
                {mapping.map((row) => (
                  <tr key={row.id} data-group-role={row.group}>
                    <Td className="font-mono text-xs break-all text-ink">{row.group}</Td>
                    <Td className="text-ink">{t.members.roleNames[row.role]}</Td>
                    <Td className="text-right">
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => unmap(row)}
                        loading={remove.isPending && remove.variables?.id === row.id}
                        aria-label={t.sso.unmapGroup(row.group)}
                        data-action="unmap-group"
                      >
                        {t.sso.unmap}
                      </Button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
          <form onSubmit={submit} noValidate aria-label={t.sso.groupRolesTitle} className="grid items-start gap-3 sm:grid-cols-[1fr_10rem_auto]">
            <Field
              label={t.sso.group}
              value={group}
              placeholder={SSO_GROUP_EXAMPLE}
              hint={t.sso.groupHint}
              error={fields.group}
              onChange={(event) => setGroup(event.target.value)}
            />
            <Select label={t.sso.role} value={role} error={fields.role} onChange={(event) => setRole(event.target.value as GrantedRole)}>
              {GRANTED.map((each) => (
                <option key={each} value={each}>
                  {t.members.roleNames[each]}
                </option>
              ))}
            </Select>
            <Button type="submit" className="sm:mt-6" loading={set.isPending} data-action="map-group">
              {t.sso.mapGroup}
            </Button>
          </form>
          <p className="text-sm text-ink-subtle">{t.sso.groupRolesLater}</p>
        </>
      )}
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="text-sm text-ink-muted">
          {notice}
        </p>
      )}
    </Card>
  );
}
